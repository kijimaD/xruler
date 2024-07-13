package main

import (
	"fmt"
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xcursor"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xwindow"
)

func main() {
	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}

	// Create the cursor. You can find a list of available cursors in
	// xcursor/cursordef.go.
	// We'll make an umbrella here, with an orange foreground and a blue
	// background. (The background it typically the outline of the cursor.)
	// Note that each component of the RGB color is a 16 bit color. I think
	// using the most significant byte to specify each component is good
	// enough.
	cursor, err := xcursor.CreateCursorExtra(X, xcursor.Umbrella,
		0xff00, 0x5500, 0x0000,
		0x3300, 0x6600, 0xff00)
	if err != nil {
		log.Fatal(err)
	}

	// ルートウィンドウに対してカーソルを設定
	root := xwindow.New(X, X.RootWin())
	xproto.ChangeWindowAttributes(X.Conn(), root.Id, xproto.CwCursor, []uint32{uint32(cursor)})

	// カーソルを解放（もう使わない場合）
	xproto.FreeCursor(X.Conn(), cursor)

	// マウスイベントのリスナーを追加
	xevent.MotionNotifyFun(
		func(X *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
			log.Printf("Mouse moved to (%d, %d)", ev.EventX, ev.EventY)
		}).Connect(X, X.RootWin())

	conn, err := xgb.NewConn()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	go func() {
		for {
			// TODO: パフォーマンスの問題がある。移動時に毎回実行したい
			getCursor(conn)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// イベントループを開始
	xevent.Main(X)
}

// カーソルの位置を取得
func getCursor(conn *xgb.Conn) {
	// ルートウィンドウの取得
	setup := xproto.Setup(conn)
	root := setup.DefaultScreen(conn).Root

	reply, err := xproto.QueryPointer(conn, root).Reply()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cursor position: (%d, %d)\n", reply.RootX, reply.RootY)
}
