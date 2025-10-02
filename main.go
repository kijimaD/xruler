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
	hideHeight      = 500                      // カーソル上下の隠す領域の高さ（ピクセル）
	cursorHeight    = 50                       // カーソル領域の高さ（ピクセル）
	overlayColor    = 0x000000                 // オーバーレイの色（グレー）
	opacityPercent  = 94                       // ウィンドウの不透明度（パーセント: 0-100）
	pollInterval    = 16 * time.Millisecond    // カーソル位置のポーリング間隔（約60fps）
	xfixesMajor     = 6                        // XFixes拡張のメジャーバージョン
	xfixesMinor     = 0                        // XFixes拡張のマイナーバージョン
	extensionXFIXES = "XFIXES"                 // XFixes拡張の名前
	atomOpacity     = "_NET_WM_WINDOW_OPACITY" // ウィンドウ不透明度を設定するアトム名
)

// Ruler X Window System上でカーソル位置を追従する水平ルーラー
type Ruler struct {
	xConn        *xgb.Conn       // X11プロトコル接続
	xuConn       *xgbutil.XUtil  // xgbutilユーティリティ接続
	topWin       *xwindow.Window // 上側のオーバーレイウィンドウ
	bottomWin    *xwindow.Window // 下側のオーバーレイウィンドウ
	screenWidth  int             // 画面の幅
	screenHeight int             // 画面の高さ
}

func main() {
	ruler := &Ruler{}
	defer ruler.Close()

	if err := ruler.init(); err != nil {
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

	for {
		// カーソル位置を取得
		_, cy := r.getCursor()

		// 位置が変わった時のみ更新（不要な描画を削減）
		if cy != lastY {
			r.updateWindows(cy)
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

// createWindows 上下2つのオーバーレイウィンドウを作成
func (r *Ruler) createWindows() error {
	var err error

	// 上側のウィンドウを作成
	r.topWin, err = xwindow.Generate(r.xuConn)
	if err != nil {
		return err
	}

	if err := r.topWin.CreateChecked(
		r.xuConn.RootWin(),
		0, 0, // 初期位置
		r.screenWidth, 1, // 初期サイズ（高さは後で更新）
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		overlayColor,
		1, // OverrideRedirect
	); err != nil {
		return err
	}

	// 下側のウィンドウを作成
	r.bottomWin, err = xwindow.Generate(r.xuConn)
	if err != nil {
		return err
	}

	if err := r.bottomWin.CreateChecked(
		r.xuConn.RootWin(),
		0, 0, // 初期位置
		r.screenWidth, 1, // 初期サイズ（高さは後で更新）
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		overlayColor,
		1, // OverrideRedirect
	); err != nil {
		return err
	}

	// ウィンドウを表示
	r.topWin.Map()
	r.bottomWin.Map()
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

	// 両方のウィンドウの入力シェイプをこのリージョンに設定
	topID := xproto.Window(r.topWin.Id)
	if err := xfixes.SetWindowShapeRegionChecked(r.xConn, topID, shape.SkInput, 0, 0, region).Check(); err != nil {
		return err
	}

	bottomID := xproto.Window(r.bottomWin.Id)
	if err := xfixes.SetWindowShapeRegionChecked(r.xConn, bottomID, shape.SkInput, 0, 0, region).Check(); err != nil {
		return err
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
		32, // 32ビット値
		1,  // 1要素
		opacityBytes,
	).Check(); err != nil {
		return err
	}

	// 下側のウィンドウに不透明度を設定
	bottomID := xproto.Window(r.bottomWin.Id)
	if err := xproto.ChangePropertyChecked(
		r.xConn,
		xproto.PropModeReplace,
		bottomID,
		atom.Atom,
		xproto.AtomCardinal,
		32, // 32ビット値
		1,  // 1要素
		opacityBytes,
	).Check(); err != nil {
		return err
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

// updateWindows カーソル位置に応じてウィンドウの位置とサイズを更新
func (r *Ruler) updateWindows(cursorY int) {
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

	// 下側のウィンドウを更新
	if bottomHeight > 0 {
		bottomID := xproto.Window(r.bottomWin.Id)
		xproto.ConfigureWindow(r.xConn, bottomID,
			xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, uint32(bottomStart), uint32(r.screenWidth), uint32(bottomHeight)})
	}

	r.xConn.Sync()
}
