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


// Ruler X Window System上でカーソル位置を追従する水平ルーラー
type Ruler struct {
	xConn        *xgb.Conn         // X11プロトコル接続
	xuConn       *xgbutil.XUtil    // xgbutilユーティリティ接続
	windows      []*xwindow.Window // ウィンドウリスト
	screenWidth  int               // 画面の幅
	screenHeight int               // 画面の高さ
	mode         Mode              // 動作モード
	visible      bool              // 表示状態
}

// New ルーラーを作成
func New(mode Mode) *Ruler {
	return &Ruler{
		mode:    mode,
		visible: true,
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
				r.mode.UpdateWindows(r.xConn, r.windows, cy, r.screenWidth, r.screenHeight)
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
		for _, win := range r.windows {
			win.Map()
		}
		log.Println("ルーラー表示: ON")
	} else {
		for _, win := range r.windows {
			win.Unmap()
		}
		log.Println("ルーラー表示: OFF")
	}
}

func (r *Ruler) createWindows() error {
	var err error

	r.windows, err = r.mode.CreateWindows(r.xuConn, r.screenWidth, r.screenHeight)
	if err != nil {
		return err
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

	for _, win := range r.windows {
		winID := xproto.Window(win.Id)
		if err := xfixes.SetWindowShapeRegionChecked(r.xConn, winID, shape.SkInput, 0, 0, region).Check(); err != nil {
			return err
		}
	}

	return nil
}

func (r *Ruler) setupTransparency() error {
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return err
	}

	opacityPercent := r.mode.GetOpacity()
	maxOpacity := float64(uint32(0xFFFFFFFF))
	opacity := opacityPercent / 100.0 * maxOpacity
	opacityValue := uint32(opacity)

	opacityBytes := []byte{
		byte(opacityValue & 0xFF),
		byte((opacityValue >> 8) & 0xFF),
		byte((opacityValue >> 16) & 0xFF),
		byte((opacityValue >> 24) & 0xFF),
	}

	for _, win := range r.windows {
		winID := xproto.Window(win.Id)
		if err := xproto.ChangePropertyChecked(
			r.xConn,
			xproto.PropModeReplace,
			winID,
			atom.Atom,
			xproto.AtomCardinal,
			32,
			1,
			opacityBytes,
		).Check(); err != nil {
			return err
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

