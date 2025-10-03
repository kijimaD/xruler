package ruler

import (
	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xwindow"
)

// RulerModeConfig ルーラーモードの設定
type RulerModeConfig struct {
	RulerHeight    int     // ルーラーの高さ（ピクセル）
	RulerColor     uint32  // ルーラーの色
	OpacityPercent float64 // ウィンドウの不透明度（パーセント: 0-100）
}

// DefaultRulerModeConfig デフォルトのルーラーモード設定
func DefaultRulerModeConfig() RulerModeConfig {
	return RulerModeConfig{
		RulerHeight:    40,
		RulerColor:     0x808080,
		OpacityPercent: 94,
	}
}

// GetOpacity 不透明度を返す
func (c RulerModeConfig) GetOpacity() float64 {
	return c.OpacityPercent
}

// CreateWindows ウィンドウを作成
func (c RulerModeConfig) CreateWindows(xuConn *xgbutil.XUtil, screenWidth, screenHeight int) ([]*xwindow.Window, error) {
	windows := make([]*xwindow.Window, 1)

	topWin, err := xwindow.Generate(xuConn)
	if err != nil {
		return nil, err
	}
	if err := topWin.CreateChecked(
		xuConn.RootWin(),
		0, 0,
		screenWidth, c.RulerHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		c.RulerColor,
		1,
	); err != nil {
		return nil, err
	}
	windows[0] = topWin

	topWin.Map()

	return windows, nil
}

// UpdateWindows カーソル位置に応じてウィンドウを更新
func (c RulerModeConfig) UpdateWindows(xConn *xgb.Conn, windows []*xwindow.Window, cursorY, screenWidth, screenHeight int) {
	topWin := windows[0]
	rulerY := cursorY - c.RulerHeight/2

	topID := xproto.Window(topWin.Id)
	xproto.ConfigureWindow(xConn, topID,
		xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{0, uint32(rulerY), uint32(screenWidth), uint32(c.RulerHeight)})

	xConn.Sync()
}
