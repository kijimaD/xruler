package main

import (
	"flag"
	"fmt"
	"log"
	"os"
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
	hideHeight      = 200                      // カーソル上下の隠す領域の高さ（ピクセル）
	cursorHeight    = 50                       // カーソル領域の高さ（ピクセル）
	rulerHeight     = 40                       // ルーラーモードの高さ（ピクセル）
	borderHeight    = 2                        // 枠線の高さ（ピクセル）
	overlayColor    = 0xf0f0f0                 // オーバーレイの色（黒）
	borderColor     = 0x000000                 // 枠線の色（黒）
	rulerColor      = 0x808080                 // ルーラーの色（グレー）
	opacityPercent  = 94                       // ウィンドウの不透明度（パーセント: 0-100）
	pollInterval    = 16 * time.Millisecond    // カーソル位置のポーリング間隔（約60fps）
	xfixesMajor     = 6                        // XFixes拡張のメジャーバージョン
	xfixesMinor     = 0                        // XFixes拡張のマイナーバージョン
	extensionXFIXES = "XFIXES"                 // XFixes拡張の名前
	atomOpacity     = "_NET_WM_WINDOW_OPACITY" // ウィンドウ不透明度を設定するアトム名
)

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
}

func main() {
	// コマンドライン引数の定義
	modeFlag := flag.String("mode", "ruler", "動作モード: ruler (半透明ルーラー) または hide (上下を隠す)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s              # 半透明の水平線を表示（デフォルト）\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -mode=ruler   # 半透明の水平線を表示\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -mode=hide    # カーソル上下を暗くする\n", os.Args[0])
	}
	flag.Parse()

	// モードの解析
	var mode Mode
	switch *modeFlag {
	case "hide":
		mode = ModeHide
	case "ruler":
		mode = ModeRuler
	default:
		fmt.Fprintf(os.Stderr, "Error: Invalid mode '%s'. Use 'hide' or 'ruler'.\n", *modeFlag)
		flag.Usage()
		os.Exit(1)
	}

	ruler := &Ruler{mode: mode, visible: true}
	defer ruler.Close()

	if err := ruler.init(); err != nil {
		log.Fatal(err)
	}

	// キーボードイベントの設定
	if err := ruler.setupKeyboard(); err != nil {
		log.Fatal(err)
	}

	ruler.run()
}

// Close X接続を閉じる
func (r *Ruler) Close() {
	if r.xConn != nil {
		r.xConn.Close()
	}
}

// run メインループ：カーソル位置を追従してウィンドウ位置を更新
func (r *Ruler) run() {
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

		time.Sleep(pollInterval)
	}
}

// init ルーラーの初期化：X接続の確立とウィンドウの設定
func (r *Ruler) init() error {
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

// createWindows ウィンドウを作成
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
			overlayColor,
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
			r.screenWidth, borderHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			borderColor,
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
			r.screenWidth, borderHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			borderColor,
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
			overlayColor,
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
			r.screenWidth, rulerHeight,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			rulerColor,
			1,
		); err != nil {
			return err
		}

		r.topWin.Map()
	}

	r.xConn.Sync()
	return nil
}

// getScreenSize 画面のサイズ（幅、高さ）を取得
func (r *Ruler) getScreenSize() (int, int) {
	setup := xproto.Setup(r.xConn)
	screen := setup.DefaultScreen(r.xConn)
	return int(screen.WidthInPixels), int(screen.HeightInPixels)
}

// setupClickThrough クリックスルーを設定（ルーラーがマウス操作を邪魔しないようにする）
func (r *Ruler) setupClickThrough() error {
	// XFIXES拡張が利用可能か確認
	extension, err := xproto.QueryExtension(r.xConn, uint16(len(extensionXFIXES)), extensionXFIXES).Reply()
	if err != nil || !extension.Present {
		return err
	}

	// XFIXES拡張を初期化
	if err := xfixes.Init(r.xConn); err != nil {
		return err
	}

	// XFixesのバージョンを問い合わせる
	// MEMO: 必須。なぜかここを実行するとCreateRegionChecked()でリクエストエラーにならなくなる
	if _, err := xfixes.QueryVersion(r.xConn, xfixesMajor, xfixesMinor).Reply(); err != nil {
		return err
	}

	// 空の入力リージョンを作成
	region, err := xfixes.NewRegionId(r.xConn)
	if err != nil {
		return err
	}
	defer xfixes.DestroyRegion(r.xConn, region)

	// サイズ0の矩形でリージョンを作成（これによりクリックイベントを素通りさせる）
	if err := xfixes.CreateRegionChecked(r.xConn, region, []xproto.Rectangle{{}}).Check(); err != nil {
		return err
	}

	// ウィンドウの入力シェイプをこのリージョンに設定
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

// setupTransparency ウィンドウの透明度を設定
func (r *Ruler) setupTransparency() error {
	// _NET_WM_WINDOW_OPACITYアトムを取得
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return err
	}

	// 不透明度を計算（0xFFFFFFFF = 完全不透明）
	maxOpacity := float64(uint32(0xFFFFFFFF))
	opacity := float64(opacityPercent) / 100.0 * maxOpacity
	opacityValue := uint32(opacity)

	// uint32値をリトルエンディアンのバイト列に変換
	opacityBytes := []byte{
		byte(opacityValue & 0xFF),
		byte((opacityValue >> 8) & 0xFF),
		byte((opacityValue >> 16) & 0xFF),
		byte((opacityValue >> 24) & 0xFF),
	}

	// 上側のウィンドウに不透明度を設定
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

	// 隠すモードの場合、枠線と下側のウィンドウにも設定
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

// getCursor カーソルの位置（X座標、Y座標）を取得
func (r *Ruler) getCursor() (int, int) {
	// ルートウィンドウを取得
	setup := xproto.Setup(r.xConn)
	root := setup.DefaultScreen(r.xConn).Root

	// カーソル位置を問い合わせ
	reply, err := xproto.QueryPointer(r.xConn, root).Reply()
	if err != nil {
		log.Fatal(err)
	}

	return int(reply.RootX), int(reply.RootY)
}

// updateWindowsHideMode 隠すモード：カーソル位置に応じてウィンドウの位置とサイズを更新
func (r *Ruler) updateWindowsHideMode(cursorY int) {
	// カーソル領域の中心を基準に計算
	cursorTop := cursorY - cursorHeight/2
	cursorBottom := cursorY + cursorHeight/2

	// 上側の隠す領域: (cursorTop - hideHeight) ～ cursorTop
	topStart := max(0, cursorTop-hideHeight)
	topEnd := cursorTop
	topHeight := topEnd - topStart

	// 下側の隠す領域: cursorBottom ～ (cursorBottom + hideHeight)
	bottomStart := cursorBottom
	bottomEnd := min(r.screenHeight, cursorBottom+hideHeight)
	bottomHeight := bottomEnd - bottomStart

	// 上側のウィンドウを更新
	if topHeight > 0 {
		topID := xproto.Window(r.topWin.Id)
		xproto.ConfigureWindow(r.xConn, topID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(topStart), uint32(r.screenWidth), uint32(topHeight)})
	}

	// 上側の枠線ウィンドウを更新（カーソル領域の上端）
	if r.topBorderWin != nil {
		topBorderID := xproto.Window(r.topBorderWin.Id)
		xproto.ConfigureWindow(r.xConn, topBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorTop), uint32(r.screenWidth), borderHeight})
	}

	// 下側の枠線ウィンドウを更新（カーソル領域の下端）
	if r.bottomBorderWin != nil {
		bottomBorderID := xproto.Window(r.bottomBorderWin.Id)
		xproto.ConfigureWindow(r.xConn, bottomBorderID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(cursorBottom - borderHeight), uint32(r.screenWidth), borderHeight})
	}

	// 下側のウィンドウを更新
	if bottomHeight > 0 {
		bottomID := xproto.Window(r.bottomWin.Id)
		xproto.ConfigureWindow(r.xConn, bottomID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(bottomStart), uint32(r.screenWidth), uint32(bottomHeight)})
	}

	r.xConn.Sync()
}

// updateWindowsRulerMode ルーラーモード：カーソル位置に水平線を表示
func (r *Ruler) updateWindowsRulerMode(cursorY int) {
	// カーソル位置を中心にルーラーを配置
	rulerY := cursorY - rulerHeight/2

	topID := xproto.Window(r.topWin.Id)
	xproto.ConfigureWindow(r.xConn, topID,
		xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{0, uint32(rulerY), uint32(r.screenWidth), uint32(rulerHeight)})

	r.xConn.Sync()
}
