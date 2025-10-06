package ruler

import (
	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xwindow"
)

// HideModeConfig 隠すモードの設定
type HideModeConfig struct {
	HideHeight     int     // カーソル上下の隠す領域の高さ（ピクセル）
	CursorHeight   int     // カーソル領域の高さ（ピクセル）
	BorderHeight   int     // 枠線の高さ（ピクセル）
	OverlayColor   uint32  // オーバーレイの色
	BorderColor    uint32  // 枠線の色
	OpacityPercent float64 // ウィンドウの不透明度（パーセント: 0-100）
}

// DefaultHideModeConfig デフォルトの隠すモード設定
func DefaultHideModeConfig() HideModeConfig {
	return HideModeConfig{
		HideHeight:     400,
		CursorHeight:   80,
		BorderHeight:   2,
		OverlayColor:   0xf0f0f0,
		BorderColor:    0x000000,
		OpacityPercent: 100,
	}
}

// GetOpacity 不透明度を返す
func (c HideModeConfig) GetOpacity() float64 {
	return c.OpacityPercent
}

// CreateWindows ウィンドウを作成
func (c HideModeConfig) CreateWindows(xuConn *xgbutil.XUtil, screenWidth, screenHeight int) ([]*xwindow.Window, error) {
	windows := make([]*xwindow.Window, 4)

	// 上側のオーバーレイウィンドウ
	topWin, err := xwindow.Generate(xuConn)
	if err != nil {
		return nil, err
	}
	if err := topWin.CreateChecked(
		xuConn.RootWin(),
		0, 0,
		screenWidth, 1,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		c.OverlayColor,
		1,
	); err != nil {
		return nil, err
	}
	windows[0] = topWin

	// 上側の枠線ウィンドウ
	topBorderWin, err := xwindow.Generate(xuConn)
	if err != nil {
		return nil, err
	}
	if err := topBorderWin.CreateChecked(
		xuConn.RootWin(),
		0, 0,
		screenWidth, c.BorderHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		c.BorderColor,
		1,
	); err != nil {
		return nil, err
	}
	windows[1] = topBorderWin

	// 下側の枠線ウィンドウ
	bottomBorderWin, err := xwindow.Generate(xuConn)
	if err != nil {
		return nil, err
	}
	if err := bottomBorderWin.CreateChecked(
		xuConn.RootWin(),
		0, 0,
		screenWidth, c.BorderHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		c.BorderColor,
		1,
	); err != nil {
		return nil, err
	}
	windows[2] = bottomBorderWin

	// 下側のオーバーレイウィンドウ
	bottomWin, err := xwindow.Generate(xuConn)
	if err != nil {
		return nil, err
	}
	if err := bottomWin.CreateChecked(
		xuConn.RootWin(),
		0, 0,
		screenWidth, 1,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		c.OverlayColor,
		1,
	); err != nil {
		return nil, err
	}
	windows[3] = bottomWin

	for _, win := range windows {
		win.Map()
	}

	return windows, nil
}

// UpdateWindows カーソル位置に応じてウィンドウを更新
func (c HideModeConfig) UpdateWindows(xConn *xgb.Conn, windows []*xwindow.Window, cursorY, screenWidth, screenHeight int) {
	topWin := windows[0]
	topBorderWin := windows[1]
	bottomBorderWin := windows[2]
	bottomWin := windows[3]

	cursorTop := cursorY - c.CursorHeight/2
	cursorBottom := cursorY + c.CursorHeight/2

	topStart := max(0, cursorTop-c.HideHeight)
	topEnd := cursorTop
	topHeight := topEnd - topStart

	bottomStart := cursorBottom
	bottomEnd := min(screenHeight, cursorBottom+c.HideHeight)
	bottomHeight := bottomEnd - bottomStart

	if topHeight > 0 {
		topID := xproto.Window(topWin.Id)
		xproto.ConfigureWindow(xConn, topID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(topStart), uint32(screenWidth), uint32(topHeight)})
	}

	if topBorderWin != nil {
		topBorderID := xproto.Window(topBorderWin.Id)
		xproto.ConfigureWindow(xConn, topBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorTop), uint32(screenWidth), uint32(c.BorderHeight)})
	}

	if bottomBorderWin != nil {
		bottomBorderID := xproto.Window(bottomBorderWin.Id)
		xproto.ConfigureWindow(xConn, bottomBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorBottom - c.BorderHeight), uint32(screenWidth), uint32(c.BorderHeight)})
	}

	if bottomHeight > 0 {
		bottomID := xproto.Window(bottomWin.Id)
		xproto.ConfigureWindow(xConn, bottomID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(bottomStart), uint32(screenWidth), uint32(bottomHeight)})
	}

	xConn.Sync()
}
