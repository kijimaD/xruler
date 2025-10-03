package ruler

import (
	"log"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/shape"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/keybind"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xwindow"
)

const (
	PollInterval    = 16 * time.Millisecond // カーソル位置のポーリング間隔（約60fps）
	xfixesMajor     = 6                     // XFixes拡張のメジャーバージョン
	xfixesMinor     = 0                     // XFixes拡張のマイナーバージョン
	extensionXFIXES = "XFIXES"              // XFixes拡張の名前
	atomOpacity     = "_NET_WM_WINDOW_OPACITY" // ウィンドウ不透明度を設定するアトム名
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

// RulerModeConfig ルーラーモードの設定
type RulerModeConfig struct {
	RulerHeight    int     // ルーラーの高さ（ピクセル）
	RulerColor     uint32  // ルーラーの色
	OpacityPercent float64 // ウィンドウの不透明度（パーセント: 0-100）
}

// DefaultHideModeConfig デフォルトの隠すモード設定
func DefaultHideModeConfig() HideModeConfig {
	return HideModeConfig{
		HideHeight:     200,
		CursorHeight:   50,
		BorderHeight:   2,
		OverlayColor:   0xf0f0f0,
		BorderColor:    0x000000,
		OpacityPercent: 94,
	}
}

// DefaultRulerModeConfig デフォルトのルーラーモード設定
func DefaultRulerModeConfig() RulerModeConfig {
	return RulerModeConfig{
		RulerHeight:    40,
		RulerColor:     0x808080,
		OpacityPercent: 94,
	}
}

// Mode 動作モード
type Mode int

const (
	ModeHide  Mode = iota // 隠すモード（上下を暗くする）
	ModeRuler             // ルーラーモード（半透明の線を表示）
)

// Ruler X Window System上でカーソル位置を追従する水平ルーラー
type Ruler struct {
	xConn           *xgb.Conn       // X11プロトコル接続
	xuConn          *xgbutil.XUtil  // xgbutilユーティリティ接続
	topWin          *xwindow.Window // 上側のオーバーレイウィンドウ
	topBorderWin    *xwindow.Window // 上側の枠線ウィンドウ（隠すモードのみ）
	bottomBorderWin *xwindow.Window // 下側の枠線ウィンドウ（隠すモードのみ）
	bottomWin       *xwindow.Window // 下側のオーバーレイウィンドウ（またはルーラーウィンドウ）
	screenWidth     int             // 画面の幅
	screenHeight    int             // 画面の高さ
	mode            Mode            // 動作モード
	visible         bool            // 表示状態
	hideConfig      HideModeConfig  // 隠すモードの設定
	rulerConfig     RulerModeConfig // ルーラーモードの設定
}

// New ルーラーを作成
func New(mode Mode) *Ruler {
	return &Ruler{
		mode:        mode,
		visible:     true,
		hideConfig:  DefaultHideModeConfig(),
		rulerConfig: DefaultRulerModeConfig(),
	}
}

// Close X接続を閉じる
func (r *Ruler) Close() {
	if r.xConn != nil {
		r.xConn.Close()
	}
}

// Run メインループ：カーソル位置を追従してウィンドウ位置を更新
func (r *Ruler) Run() {
	var lastY int = -1

	go xevent.Main(r.xuConn)

	for {
		// カーソル位置を取得
		_, cy := r.getCursor()

		// 位置が変わった時のみ更新（不要な描画を削減）
		if cy != lastY {
			if r.visible {
				if r.mode == ModeHide {
					r.updateWindowsHideMode(cy)
				} else {
					r.updateWindowsRulerMode(cy)
				}
			}
			lastY = cy
		}

		time.Sleep(PollInterval)
	}
}

// Init ルーラーの初期化：X接続の確立とウィンドウの設定
func (r *Ruler) Init() error {
	var err error

	// xgbutilユーティリティ接続を確立
	r.xuConn, err = xgbutil.NewConn()
	if err != nil {
		return err
	}

	// X11プロトコル接続を確立
	r.xConn, err = xgb.NewConn()
	if err != nil {
		return err
	}
	r.xConn.Sync()

	// 画面サイズを取得
	r.screenWidth, r.screenHeight = r.getScreenSize()

	// 上下2つのウィンドウを作成
	if err := r.createWindows(); err != nil {
		return err
	}

	// クリックスルー設定（ルーラーがマウスクリックを邪魔しないようにする）
	if err := r.setupClickThrough(); err != nil {
		return err
	}

	// 透明度を設定
	if err := r.setupTransparency(); err != nil {
		return err
	}

	// キーボードイベントの設定
	if err := r.setupKeyboard(); err != nil {
		return err
	}

	return nil
}

// setupKeyboard キーボードイベントを設定
func (r *Ruler) setupKeyboard() error {
	// keybindを初期化
	keybind.Initialize(r.xuConn)

	// ルートウィンドウでグローバルにキーをキャプチャ
	err := keybind.KeyPressFun(
		func(X *xgbutil.XUtil, e xevent.KeyPressEvent) {
			r.toggleVisibility()
		}).Connect(r.xuConn, r.xuConn.RootWin(), "Control-Shift-space", true)

	if err != nil {
		return err
	}

	log.Println("キーバインド設定完了: Ctrl+Shift+Space でトグル")

	return nil
}

// toggleVisibility 表示状態を切り替え
func (r *Ruler) toggleVisibility() {
	r.visible = !r.visible

	if r.visible {
		r.topWin.Map()
		if r.topBorderWin != nil {
			r.topBorderWin.Map()
		}
		if r.bottomBorderWin != nil {
			r.bottomBorderWin.Map()
		}
		if r.bottomWin != nil {
			r.bottomWin.Map()
		}
		log.Println("ルーラー表示: ON")
	} else {
		r.topWin.Unmap()
		if r.topBorderWin != nil {
			r.topBorderWin.Unmap()
		}
		if r.bottomBorderWin != nil {
			r.bottomBorderWin.Unmap()
		}
		if r.bottomWin != nil {
			r.bottomWin.Unmap()
		}
		log.Println("ルーラー表示: OFF")
	}
}

func (r *Ruler) createWindows() error {
	var err error

	if r.mode == ModeHide {
		// 隠すモード：上下2つのウィンドウ + 2つの枠線ウィンドウ
		r.topWin, err = xwindow.Generate(r.xuConn)
		if err != nil {
			return err
		}

		if err := r.topWin.CreateChecked(
			r.xuConn.RootWin(),
			0, 0,
			r.screenWidth, 1,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			r.hideConfig.OverlayColor,
			1,
		); err != nil {
			return err
		}

		r.topBorderWin, err = xwindow.Generate(r.xuConn)
		if err != nil {
			return err
		}

		if err := r.topBorderWin.CreateChecked(
			r.xuConn.RootWin(),
			0, 0,
			r.screenWidth, r.hideConfig.BorderHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			r.hideConfig.BorderColor,
			1,
		); err != nil {
			return err
		}

		r.bottomBorderWin, err = xwindow.Generate(r.xuConn)
		if err != nil {
			return err
		}

		if err := r.bottomBorderWin.CreateChecked(
			r.xuConn.RootWin(),
			0, 0,
			r.screenWidth, r.hideConfig.BorderHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			r.hideConfig.BorderColor,
			1,
		); err != nil {
			return err
		}

		r.bottomWin, err = xwindow.Generate(r.xuConn)
		if err != nil {
			return err
		}

		if err := r.bottomWin.CreateChecked(
			r.xuConn.RootWin(),
			0, 0,
			r.screenWidth, 1,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			r.hideConfig.OverlayColor,
			1,
		); err != nil {
			return err
		}

		r.topWin.Map()
		r.topBorderWin.Map()
		r.bottomBorderWin.Map()
		r.bottomWin.Map()
	} else {
		// ルーラーモード：1つの水平線
		r.topWin, err = xwindow.Generate(r.xuConn)
		if err != nil {
			return err
		}

		if err := r.topWin.CreateChecked(
			r.xuConn.RootWin(),
			0, 0,
			r.screenWidth, r.rulerConfig.RulerHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			r.rulerConfig.RulerColor,
			1,
		); err != nil {
			return err
		}

		r.topWin.Map()
	}

	r.xConn.Sync()
	return nil
}

func (r *Ruler) getScreenSize() (int, int) {
	setup := xproto.Setup(r.xConn)
	screen := setup.DefaultScreen(r.xConn)
	return int(screen.WidthInPixels), int(screen.HeightInPixels)
}

func (r *Ruler) setupClickThrough() error {
	extension, err := xproto.QueryExtension(r.xConn, uint16(len(extensionXFIXES)), extensionXFIXES).Reply()
	if err != nil || !extension.Present {
		return err
	}

	if err := xfixes.Init(r.xConn); err != nil {
		return err
	}

	if _, err := xfixes.QueryVersion(r.xConn, xfixesMajor, xfixesMinor).Reply(); err != nil {
		return err
	}

	region, err := xfixes.NewRegionId(r.xConn)
	if err != nil {
		return err
	}
	defer xfixes.DestroyRegion(r.xConn, region)

	if err := xfixes.CreateRegionChecked(r.xConn, region, []xproto.Rectangle{{}}).Check(); err != nil {
		return err
	}

	topID := xproto.Window(r.topWin.Id)
	if err := xfixes.SetWindowShapeRegionChecked(r.xConn, topID, shape.SkInput, 0, 0, region).Check(); err != nil {
		return err
	}

	if r.mode == ModeHide {
		if r.topBorderWin != nil {
			topBorderID := xproto.Window(r.topBorderWin.Id)
			if err := xfixes.SetWindowShapeRegionChecked(r.xConn, topBorderID, shape.SkInput, 0, 0, region).Check(); err != nil {
				return err
			}
		}
		if r.bottomBorderWin != nil {
			bottomBorderID := xproto.Window(r.bottomBorderWin.Id)
			if err := xfixes.SetWindowShapeRegionChecked(r.xConn, bottomBorderID, shape.SkInput, 0, 0, region).Check(); err != nil {
				return err
			}
		}
		if r.bottomWin != nil {
			bottomID := xproto.Window(r.bottomWin.Id)
			if err := xfixes.SetWindowShapeRegionChecked(r.xConn, bottomID, shape.SkInput, 0, 0, region).Check(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Ruler) setupTransparency() error {
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return err
	}

	var opacityPercent float64
	if r.mode == ModeHide {
		opacityPercent = r.hideConfig.OpacityPercent
	} else {
		opacityPercent = r.rulerConfig.OpacityPercent
	}

	maxOpacity := float64(uint32(0xFFFFFFFF))
	opacity := opacityPercent / 100.0 * maxOpacity
	opacityValue := uint32(opacity)

	opacityBytes := []byte{
		byte(opacityValue & 0xFF),
		byte((opacityValue >> 8) & 0xFF),
		byte((opacityValue >> 16) & 0xFF),
		byte((opacityValue >> 24) & 0xFF),
	}

	topID := xproto.Window(r.topWin.Id)
	if err := xproto.ChangePropertyChecked(
		r.xConn,
		xproto.PropModeReplace,
		topID,
		atom.Atom,
		xproto.AtomCardinal,
		32,
		1,
		opacityBytes,
	).Check(); err != nil {
		return err
	}

	if r.mode == ModeHide {
		if r.topBorderWin != nil {
			topBorderID := xproto.Window(r.topBorderWin.Id)
			if err := xproto.ChangePropertyChecked(
				r.xConn,
				xproto.PropModeReplace,
				topBorderID,
				atom.Atom,
				xproto.AtomCardinal,
				32,
				1,
				opacityBytes,
			).Check(); err != nil {
				return err
			}
		}
		if r.bottomBorderWin != nil {
			bottomBorderID := xproto.Window(r.bottomBorderWin.Id)
			if err := xproto.ChangePropertyChecked(
				r.xConn,
				xproto.PropModeReplace,
				bottomBorderID,
				atom.Atom,
				xproto.AtomCardinal,
				32,
				1,
				opacityBytes,
			).Check(); err != nil {
				return err
			}
		}
		if r.bottomWin != nil {
			bottomID := xproto.Window(r.bottomWin.Id)
			if err := xproto.ChangePropertyChecked(
				r.xConn,
				xproto.PropModeReplace,
				bottomID,
				atom.Atom,
				xproto.AtomCardinal,
				32,
				1,
				opacityBytes,
			).Check(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Ruler) getCursor() (int, int) {
	setup := xproto.Setup(r.xConn)
	root := setup.DefaultScreen(r.xConn).Root

	reply, err := xproto.QueryPointer(r.xConn, root).Reply()
	if err != nil {
		log.Fatal(err)
	}

	return int(reply.RootX), int(reply.RootY)
}

func (r *Ruler) updateWindowsHideMode(cursorY int) {
	cursorTop := cursorY - r.hideConfig.CursorHeight/2
	cursorBottom := cursorY + r.hideConfig.CursorHeight/2

	topStart := max(0, cursorTop-r.hideConfig.HideHeight)
	topEnd := cursorTop
	topHeight := topEnd - topStart

	bottomStart := cursorBottom
	bottomEnd := min(r.screenHeight, cursorBottom+r.hideConfig.HideHeight)
	bottomHeight := bottomEnd - bottomStart

	if topHeight > 0 {
		topID := xproto.Window(r.topWin.Id)
		xproto.ConfigureWindow(r.xConn, topID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(topStart), uint32(r.screenWidth), uint32(topHeight)})
	}

	if r.topBorderWin != nil {
		topBorderID := xproto.Window(r.topBorderWin.Id)
		xproto.ConfigureWindow(r.xConn, topBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorTop), uint32(r.screenWidth), uint32(r.hideConfig.BorderHeight)})
	}

	if r.bottomBorderWin != nil {
		bottomBorderID := xproto.Window(r.bottomBorderWin.Id)
		xproto.ConfigureWindow(r.xConn, bottomBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorBottom - r.hideConfig.BorderHeight), uint32(r.screenWidth), uint32(r.hideConfig.BorderHeight)})
	}

	if bottomHeight > 0 {
		bottomID := xproto.Window(r.bottomWin.Id)
		xproto.ConfigureWindow(r.xConn, bottomID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(bottomStart), uint32(r.screenWidth), uint32(bottomHeight)})
	}

	r.xConn.Sync()
}

func (r *Ruler) updateWindowsRulerMode(cursorY int) {
	rulerY := cursorY - r.rulerConfig.RulerHeight/2

	topID := xproto.Window(r.topWin.Id)
	xproto.ConfigureWindow(r.xConn, topID,
		xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{0, uint32(rulerY), uint32(r.screenWidth), uint32(r.rulerConfig.RulerHeight)})

	r.xConn.Sync()
}
