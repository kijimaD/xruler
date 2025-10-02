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
	cursorHeight    = 20                    // ルーラーウィンドウの高さ（ピクセル）
	fillColor       = 0x808080              // ルーラーの背景色（グレー）
	pollInterval    = 10 * time.Millisecond // カーソル位置のポーリング間隔
	opacityValue    = 0x5a000000            // ウィンドウの不透明度（35%）
	xfixesMajor     = 6                     // XFixes拡張のメジャーバージョン
	xfixesMinor     = 0                     // XFixes拡張のマイナーバージョン
	extensionXFIXES = "XFIXES"              // XFixes拡張の名前
	atomOpacity     = "_NET_WM_WINDOW_OPACITY" // ウィンドウ不透明度を設定するアトム名
)

// Ruler X Window System上でカーソル位置を追従する水平ルーラー
type Ruler struct {
	xConn  *xgb.Conn        // X11プロトコル接続
	xuConn *xgbutil.XUtil   // xgbutilユーティリティ接続
	xWin   *xwindow.Window  // ルーラーウィンドウ
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

// run メインループ：カーソル位置を追従してルーラーウィンドウを移動
func (r *Ruler) run() {
	for {
		// カーソル位置を同期
		// TODO: パフォーマンスの問題がある。移動時だけ実行したい
		_, cy := r.getCursor()

		// ウィンドウ位置を更新（カーソルの垂直中央にルーラーを配置）
		windowID := xproto.Window(r.xWin.Id)
		xproto.ConfigureWindow(r.xConn, windowID, xproto.ConfigWindowX|xproto.ConfigWindowY,
			[]uint32{0, uint32(cy - cursorHeight/2)})

		r.xConn.Sync()

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

	// ウィンドウIDを生成
	r.xWin, err = xwindow.Generate(r.xuConn)
	if err != nil {
		return err
	}

	// X11プロトコル接続を確立
	r.xConn, err = xgb.NewConn()
	if err != nil {
		return err
	}
	r.xConn.Sync()

	// ルーラーウィンドウを作成
	if err := r.createWindow(); err != nil {
		return err
	}

	// クリックスルー設定（ルーラーがマウスクリックを邪魔しないようにする）
	if err := r.setupClickThrough(); err != nil {
		return err
	}

	// 透過設定
	if err := r.setupTransparency(); err != nil {
		return err
	}

	return nil
}

// createWindow ルーラーウィンドウを作成して表示
func (r *Ruler) createWindow() error {
	screenWidth := r.getScreenWidth()

	// 画面全体の幅を持つウィンドウを作成
	if err := r.xWin.CreateChecked(
		r.xuConn.RootWin(),
		0,
		0,
		screenWidth,
		cursorHeight,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		fillColor,
		1, // OverrideRedirect: ウィンドウマネージャの制御を受けない
		xproto.EventMaskPointerMotion,
	); err != nil {
		return err
	}
	r.xWin.Map()

	return nil
}

// getScreenWidth 画面の幅（ピクセル）を取得
func (r *Ruler) getScreenWidth() int {
	setup := xproto.Setup(r.xConn)
	screen := setup.DefaultScreen(r.xConn)
	return int(screen.WidthInPixels)
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
	windowID := xproto.Window(r.xWin.Id)
	if err := xfixes.SetWindowShapeRegionChecked(r.xConn, windowID, shape.SkInput, 0, 0, region).Check(); err != nil {
		return err
	}

	return nil
}

// setupTransparency ウィンドウの透明度を設定
func (r *Ruler) setupTransparency() error {
	windowID := xproto.Window(r.xWin.Id)

	// _NET_WM_WINDOW_OPACITYアトムを取得
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return err
	}

	// uint32値をビッグエンディアンのバイト列に変換
	// Goライブラリでは[]byte型だが、Xプロトコルではuint32として扱われる
	opacityBytes := []byte{
		byte((opacityValue >> 24) & 0xFF),
		byte((opacityValue >> 16) & 0xFF),
		byte((opacityValue >> 8) & 0xFF),
		byte(opacityValue & 0xFF),
	}

	// ウィンドウプロパティに不透明度を設定
	if err := xproto.ChangePropertyChecked(
		r.xConn,
		xproto.PropModeReplace,
		windowID,
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
