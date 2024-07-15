package main

import (
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/shape"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xwindow"
)

const cursorWidth = 10
const cursorHeight = 10

func main() {
	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}

	win, err := xwindow.Generate(X)
	if err != nil {
		log.Fatal(err)
	}
	if err := win.CreateChecked(
		X.RootWin(),
		0,
		0,
		cursorWidth*4,
		cursorHeight*4,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		0x00000000,
		1,
		xproto.EventMaskPointerMotion,
	); err != nil {
		log.Fatal(err)
	}
	win.Map()

	// Xサーバに接続
	X2, err := xgb.NewConn()
	if err != nil {
		log.Fatal(err)
	}
	defer X2.Close()
	X2.Sync()

	// 拡張が読み込まれているか確認する
	extension, err := xproto.QueryExtension(X2, uint16(len("XFIXES")), "XFIXES").Reply()
	if err != nil {
		log.Fatalf("QueryExtension failed: %v", err)
	}
	if !extension.Present {
		log.Fatalf("XFIXES extension is not present")
	}

	// クリックできるようにする
	{
		err = xfixes.Init(X2)
		if err != nil {
			log.Fatalf("Cannot initialize XFixes extension: %v", err)
		}
		// XFixesのバージョンを問い合わせる
		// MEMO: なぜかここを実行するとCreateRegionChecked()でリクエストエラーにならなくなる
		major := uint32(6)
		minor := uint32(0)
		_, err := xfixes.QueryVersion(X2, major, minor).Reply()
		if err != nil {
			log.Fatalf("Failed to query XFixes version: %v", err)
		}

		region, err := xfixes.NewRegionId(X2)
		if err != nil {
			log.Fatalf("NewRegion failed: %v", err)
		}
		// MEMO: rectの大きさが縦横の長さが0であることが重要。これによって、描画領域がマウスクリックを邪魔しないようにするq
		cookie := xfixes.CreateRegionChecked(X2, region, []xproto.Rectangle{xproto.Rectangle{}})
		if err := cookie.Check(); err != nil {
			log.Fatalf("CreateRegionChecked failed: %v", err)
		}
		windowID := xproto.Window(win.Id)
		cookie2 := xfixes.SetWindowShapeRegionChecked(X2, windowID, shape.SkInput, 0, 0, region)
		if err := cookie2.Check(); err != nil {
			log.Fatalf("SetWindowShapeRegionChecked: %v", err)
		}
		xfixes.DestroyRegion(X2, region)
	}

	// xevent.MotionNotifyFun(
	// 	func(X *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
	// 		ev = motionNotify(X, ev)
	// 		fmt.Printf("COMPRESSED: (EventX %d, EventY %d)\n", ev.EventX, ev.EventY)

	// 	}).Connect(X, win.Id)
	// go func() {
	// 	xevent.Main(X)
	// }()

	for {
		go func() {
			// TODO: パフォーマンスの問題がある。移動時だけ実行したい
			cx, cy := getCursor(X2)

			windowID := xproto.Window(win.Id)
			xproto.ConfigureWindow(X2, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
				[]uint32{uint32(cx - cursorWidth/2), uint32(cy - cursorHeight/2)})

			X2.Sync()
		}()
		time.Sleep(10 * time.Millisecond)
	}
}

// カーソルの位置を取得
func getCursor(conn *xgb.Conn) (int, int) {
	// ルートウィンドウの取得
	setup := xproto.Setup(conn)
	root := setup.DefaultScreen(conn).Root

	reply, err := xproto.QueryPointer(conn, root).Reply()
	if err != nil {
		log.Fatal(err)
	}

	return int(reply.RootX), int(reply.RootY)
}

func motionNotify(X *xgbutil.XUtil, ev xevent.MotionNotifyEvent) xevent.MotionNotifyEvent {
	X.Sync()
	xevent.Read(X, false)
	laste := ev

	for i, ee := range xevent.Peek(X) {
		if ee.Err != nil { // This is an error, skip it.
			continue
		}

		if mn, ok := ee.Event.(xproto.MotionNotifyEvent); ok {
			if ev.Event == mn.Event && ev.Child == mn.Child &&
				ev.Detail == mn.Detail && ev.State == mn.State &&
				ev.Root == mn.Root && ev.SameScreen == mn.SameScreen {

				laste = xevent.MotionNotifyEvent{&mn}

				defer func(i int) { xevent.DequeueAt(X, i) }(i)
			}
		}
	}

	X.TimeSet(laste.Time)

	return laste
}
