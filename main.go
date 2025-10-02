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

const (
	cursorHeight   = 20
	fillColor      = 0x808080
	pollInterval   = 10 * time.Millisecond
	opacityValue   = 0x5a000000
	xfixesMajor    = 6
	xfixesMinor    = 0
	extensionXFIXES = "XFIXES"
	atomOpacity    = "_NET_WM_WINDOW_OPACITY"
)

type Ruler struct {
	xConn  *xgb.Conn
	xuConn *xgbutil.XUtil
	xWin   *xwindow.Window
}

func main() {
	ruler := &Ruler{}
	defer ruler.Close()

	if err := ruler.init(); err != nil {
		log.Fatal(err)
	}

	ruler.run()
}

func (r *Ruler) Close() {
	if r.xConn != nil {
		r.xConn.Close()
	}
}

func (r *Ruler) run() {
	for {
		// sync cursor movement
		// TODO: パフォーマンスの問題がある。移動時だけ実行したい
		_, cy := r.getCursor()

		windowID := xproto.Window(r.xWin.Id)
		xproto.ConfigureWindow(r.xConn, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
			[]uint32{0, uint32(cy - cursorHeight/2)})

		r.xConn.Sync()

		time.Sleep(pollInterval)
	}
}

func (r *Ruler) init() error {
	var err error

	// Xサーバに接続
	r.xuConn, err = xgbutil.NewConn()
	if err != nil {
		return err
	}

	r.xWin, err = xwindow.Generate(r.xuConn)
	if err != nil {
		return err
	}

	r.xConn, err = xgb.NewConn()
	if err != nil {
		return err
	}
	r.xConn.Sync()

	if err := r.createWindow(); err != nil {
		return err
	}

	if err := r.setupClickThrough(); err != nil {
		return err
	}

	if err := r.setupTransparency(); err != nil {
		return err
	}

	return nil
}

func (r *Ruler) createWindow() error {
	screenWidth := r.getScreenWidth()

	if err := r.xWin.CreateChecked(
		r.xuConn.RootWin(),
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
	r.xWin.Map()

	return nil
}

func (r *Ruler) getScreenWidth() int {
	setup := xproto.Setup(r.xConn)
	screen := setup.DefaultScreen(r.xConn)
	return int(screen.WidthInPixels)
}

func (r *Ruler) setupClickThrough() error {
	// 拡張が読み込まれているか確認する
	extension, err := xproto.QueryExtension(r.xConn, uint16(len(extensionXFIXES)), extensionXFIXES).Reply()
	if err != nil || !extension.Present {
		return err
	}

	if err := xfixes.Init(r.xConn); err != nil {
		return err
	}

	// XFixesのバージョンを問い合わせる
	// MEMO: 必須。なぜかここを実行するとCreateRegionChecked()でリクエストエラーにならなくなる
	if _, err := xfixes.QueryVersion(r.xConn, xfixesMajor, xfixesMinor).Reply(); err != nil {
		return err
	}

	region, err := xfixes.NewRegionId(r.xConn)
	if err != nil {
		return err
	}
	defer xfixes.DestroyRegion(r.xConn, region)

	// MEMO: rectの大きさが縦横の長さが0であることが重要。これによって、描画領域がマウスクリックを邪魔しないようにする
	if err := xfixes.CreateRegionChecked(r.xConn, region, []xproto.Rectangle{{}}).Check(); err != nil {
		return err
	}

	windowID := xproto.Window(r.xWin.Id)
	if err := xfixes.SetWindowShapeRegionChecked(r.xConn, windowID, shape.SkInput, 0, 0, region).Check(); err != nil {
		return err
	}

	return nil
}

func (r *Ruler) setupTransparency() error {
	windowID := xproto.Window(r.xWin.Id)
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return err
	}

	// Goライブラリでは[]byte型だが、Cライブラリだとuint32。4バイト分必要で、足りないとエラー"slice bounds out of range"になるので埋める
	opacityBytes := []byte{
		byte((opacityValue >> 24) & 0xFF),
		byte((opacityValue >> 16) & 0xFF),
		byte((opacityValue >> 8) & 0xFF),
		byte(opacityValue & 0xFF),
	}

	if err := xproto.ChangePropertyChecked(
		r.xConn,
		xproto.PropModeReplace,
		windowID,
		atom.Atom,
		xproto.AtomCardinal,
		32,
		1,
		opacityBytes,
	).Check(); err != nil {
		return err
	}

	return nil
}

// getCursor カーソルの位置を取得
func (r *Ruler) getCursor() (int, int) {
	setup := xproto.Setup(r.xConn)
	root := setup.DefaultScreen(r.xConn).Root

	reply, err := xproto.QueryPointer(r.xConn, root).Reply()
	if err != nil {
		log.Fatal(err)
	}

	return int(reply.RootX), int(reply.RootY)
}
