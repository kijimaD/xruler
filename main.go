package main

import (
	"fmt"
	"log"
	"time"

	"github.com/BurntSushi/xgb"
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
	win.Create(
		X.RootWin(),
		0,
		0,
		cursorWidth,
		cursorHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		0x00000000,
		1,
		xproto.EventMaskButtonRelease,
	)
	win.Map()

	conn, err := xgb.NewConn()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	go func() {
		for {
			// TODO: パフォーマンスの問題がある。移動時だけ実行したい
			cx, cy := getCursor(conn)
			fmt.Printf("Cursor position: (%d, %d)\n", cx, cy)

			// Xサーバに接続
			X2, err := xgb.NewConn()
			if err != nil {
				log.Fatal(err)
			}

			// xfixes拡張
			err = xfixes.Init(X2)
			if err != nil {
				log.Fatalf("Cannot initialize XFixes extension: %v", err)
			}

			windowID := xproto.Window(win.Id)
			xproto.ConfigureWindow(X2, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
				[]uint32{uint32(cx - cursorWidth/2), uint32(cy - cursorHeight/2)})

			// クリックできるようにする
			{
				rect := xproto.Rectangle{
					X:      0,
					Y:      0,
					Width:  200,
					Height: 200,
				}

				region, err := xfixes.NewRegionId(X2)
				if err != nil {
					log.Fatalf("NewRegion failed: %v", err)
				}
				xfixes.CreateRegion(X2, region, []xproto.Rectangle{rect})

				// Regionを破棄
				xfixes.DestroyRegion(X2, region)
			}

			X2.Sync()
			X2.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	xevent.Main(X)
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
