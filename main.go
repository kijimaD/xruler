package main

import (
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/shape"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xwindow"
)

const cursorHeight = 20
const fillColor = 0x808080

var xConn *xgb.Conn
var xuConn *xgbutil.XUtil
var xWin *xwindow.Window

func main() {
	defer xConn.Close()
	if err := initWindow(); err != nil {
		log.Fatal(err)
	}

	for {
		// sync cursor movement
		{
			// TODO: パフォーマンスの問題がある。移動時だけ実行したい
			_, cy := getCursor(xConn)

			windowID := xproto.Window(xWin.Id)
			xproto.ConfigureWindow(xConn, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
				[]uint32{uint32(0), uint32(cy - cursorHeight/2)})

			xConn.Sync()
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func initWindow() error {
	var err error
	xuConn, err = xgbutil.NewConn()
	if err != nil {
		return err
	}

	xWin, err = xwindow.Generate(xuConn)
	if err != nil {
		return err
	}

	// Xサーバに接続
	xConn, err = xgb.NewConn()
	if err != nil {
		return err
	}
	xConn.Sync()

	var screenWidth int
	{
		setup := xproto.Setup(xConn)
		screen := setup.DefaultScreen(xConn)
		screenWidth = int(screen.WidthInPixels)
	}

	if err := xWin.CreateChecked(
		xuConn.RootWin(),
		0,
		0,
		screenWidth,
		cursorHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		fillColor,
		1, // true
		xproto.EventMaskPointerMotion,
	); err != nil {
		return err
	}
	xWin.Map()

	// 拡張が読み込まれているか確認する
	extension, err := xproto.QueryExtension(xConn, uint16(len("XFIXES")), "XFIXES").Reply()
	if err != nil || !extension.Present {
		return err
	}

	// ignore click
	{
		err = xfixes.Init(xConn)
		if err != nil {
			return err
		}
		// XFixesのバージョンを問い合わせる
		// MEMO: 必須。なぜかここを実行するとCreateRegionChecked()でリクエストエラーにならなくなる
		major := uint32(6)
		minor := uint32(0)
		_, err := xfixes.QueryVersion(xConn, major, minor).Reply()
		if err != nil {
			return err
		}

		region, err := xfixes.NewRegionId(xConn)
		if err != nil {
			return err
		}
		// MEMO: rectの大きさが縦横の長さが0であることが重要。これによって、描画領域がマウスクリックを邪魔しないようにする
		cookie := xfixes.CreateRegionChecked(xConn, region, []xproto.Rectangle{xproto.Rectangle{}})
		if err := cookie.Check(); err != nil {
			return err
		}
		windowID := xproto.Window(xWin.Id)
		cookie2 := xfixes.SetWindowShapeRegionChecked(xConn, windowID, shape.SkInput, 0, 0, region)
		if err := cookie2.Check(); err != nil {
			return err
		}
		xfixes.DestroyRegion(xConn, region)
	}

	// set transparency
	{
		windowID := xproto.Window(xWin.Id)
		atom, err := xproto.InternAtom(xConn, true, uint16(len("_NET_WM_WINDOW_OPACITY")), "_NET_WM_WINDOW_OPACITY").Reply()
		if err != nil {
			return err
		}
		if err := xproto.ChangePropertyChecked(
			xConn,
			xproto.PropModeReplace,
			windowID,
			atom.Atom,
			xproto.AtomCardinal,
			32,
			1,
			[]byte{0x00, 0x00, 0x00, 0x5a}, // Goライブラリでは[]byte型だが、Cライブラリだとuint32。4バイト分必要で、足りないとエラー"slice bounds out of range"になるので埋める
		).Check(); err != nil {
			return err
		}
	}

	// FIXME: カーソル移動の通知で動作させたいけど、カーソルが当たってないと通知されない
	// クリックを邪魔しないように設定しているのと、両立できるのかわからない
	// xevent.MotionNotifyFun(
	// 	func(X *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
	// 		ev = motionNotify(X, ev)
	// 		fmt.Printf("COMPRESSED: (EventX %d, EventY %d)\n", ev.EventX, ev.EventY)

	// 	}).Connect(X, win.Id)
	// go func() {
	// 	xevent.Main(X)
	// }()

	return nil
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
