package main

import (
	"fmt"
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xwindow"
)

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
		10,
		10,
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
			windowID := xproto.Window(win.Id)
			xproto.ConfigureWindow(X2, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
				[]uint32{uint32(cx), uint32(cy)})
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
