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
	PollInterval     = 16 * time.Millisecond // カーソル位置のポーリング間隔（約60fps）
	TrailDuration    = 2 * time.Second       // 軌跡の表示時間
	TrailMinDistance = 1                     // 軌跡を追加する最小移動距離（ピクセル）
	TrailLineWidth   = 3                     // 軌跡の線の太さ
	TrailColor       = 0xFF0000              // 軌跡の色（赤）
	xfixesMajor      = 6                     // XFixes拡張のメジャーバージョン
	xfixesMinor      = 0                     // XFixes拡張のマイナーバージョン
	extensionXFIXES  = "XFIXES"              // XFixes拡張の名前
	atomOpacity      = "_NET_WM_WINDOW_OPACITY" // ウィンドウ不透明度を設定するアトム名
)


// TrailSegment 軌跡の線分
type TrailSegment struct {
	x1, y1    int
	x2, y2    int
	timestamp time.Time
	window    *xwindow.Window
	gc        xproto.Gcontext
}

// Ruler X Window System上でカーソル位置を追従する水平ルーラー
type Ruler struct {
	xConn        *xgb.Conn         // X11プロトコル接続
	xuConn       *xgbutil.XUtil    // xgbutilユーティリティ接続
	windows      []*xwindow.Window // ウィンドウリスト
	screenWidth  int               // 画面の幅
	screenHeight int               // 画面の高さ
	mode         Mode              // 動作モード
	visible      bool              // 表示状態
	trails       []*TrailSegment   // 軌跡のリスト
	lastTrailX   int               // 最後の軌跡のX座標
	lastTrailY   int               // 最後の軌跡のY座標
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
	var lastX, lastCY int = -1, -1

	go xevent.Main(r.xuConn)

	for {
		// カーソル位置を取得
		cx, cy := r.getCursor()

		// 位置が変わった時のみ更新（不要な描画を削減）
		if cy != lastY {
			if r.visible {
				r.mode.UpdateWindows(r.xConn, r.windows, cy, r.screenWidth, r.screenHeight)
			}
			lastY = cy
		}

		// カーソルが移動したら軌跡を追加
		if cx != lastX || cy != lastCY {
			if r.visible && lastX != -1 && lastCY != -1 {
				if r.shouldAddTrailSegment(lastX, lastCY, cx, cy) {
					r.addTrailSegment(lastX, lastCY, cx, cy)
				}
			}
			lastX, lastCY = cx, cy
		}

		r.updateTrails()
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

// shouldAddTrailSegment 軌跡セグメントを追加すべきか判定
func (r *Ruler) shouldAddTrailSegment(x1, y1, x2, y2 int) bool {
	if x1 == -1 || y1 == -1 {
		return false
	}
	dx := x2 - x1
	dy := y2 - y1
	distance := dx*dx + dy*dy
	return distance >= TrailMinDistance*TrailMinDistance
}

// addTrailSegment 軌跡セグメントを追加
func (r *Ruler) addTrailSegment(x1, y1, x2, y2 int) {
	if r.xConn == nil || r.xuConn == nil {
		return
	}

	// 線分を囲む矩形を計算
	minX := min(x1, x2) - 5
	minY := min(y1, y2) - 5
	maxX := max(x1, x2) + 5
	maxY := max(y1, y2) + 5
	width := maxX - minX
	height := maxY - minY

	// ウィンドウを生成
	win, err := xwindow.Generate(r.xuConn)
	if err != nil {
		log.Println("軌跡ウィンドウ生成エラー:", err)
		return
	}

	if err := win.CreateChecked(
		r.xuConn.RootWin(),
		minX, minY,
		width, height,
		xproto.CwBackPixel|xproto.CwOverrideRedirect,
		0x000000, // 黒背景
		1,
	); err != nil {
		log.Println("軌跡ウィンドウ作成エラー:", err)
		return
	}

	win.Map()

	// GCを作成
	gc, _ := xproto.NewGcontextId(r.xConn)
	xproto.CreateGCChecked(
		r.xConn,
		gc,
		xproto.Drawable(win.Id),
		xproto.GcForeground|xproto.GcLineWidth|xproto.GcCapStyle,
		[]uint32{TrailColor, TrailLineWidth, xproto.CapStyleRound},
	).Check()

	// ウィンドウに直接線を描画
	xproto.PolyLine(
		r.xConn,
		xproto.CoordModeOrigin,
		xproto.Drawable(win.Id),
		gc,
		[]xproto.Point{
			{int16(x1 - minX), int16(y1 - minY)},
			{int16(x2 - minX), int16(y2 - minY)},
		},
	)

	// マスクを作成して線の部分だけを表示
	setup := xproto.Setup(r.xConn)
	screen := setup.DefaultScreen(r.xConn)
	maskPixmap, _ := xproto.NewPixmapId(r.xConn)
	xproto.CreatePixmapChecked(
		r.xConn,
		1, // 1-bit depth
		maskPixmap,
		xproto.Drawable(screen.Root),
		uint16(width),
		uint16(height),
	).Check()

	maskGC, _ := xproto.NewGcontextId(r.xConn)
	xproto.CreateGCChecked(
		r.xConn,
		maskGC,
		xproto.Drawable(maskPixmap),
		xproto.GcForeground|xproto.GcBackground,
		[]uint32{0, 0},
	).Check()

	// マスクをクリア（透明）
	xproto.PolyFillRectangle(
		r.xConn,
		xproto.Drawable(maskPixmap),
		maskGC,
		[]xproto.Rectangle{{0, 0, uint16(width), uint16(height)}},
	)

	// マスクに線を描画（不透明）
	xproto.ChangeGC(r.xConn, maskGC, xproto.GcForeground|xproto.GcLineWidth|xproto.GcCapStyle,
		[]uint32{1, 8, xproto.CapStyleRound})
	xproto.PolyLine(
		r.xConn,
		xproto.CoordModeOrigin,
		xproto.Drawable(maskPixmap),
		maskGC,
		[]xproto.Point{
			{int16(x1 - minX), int16(y1 - minY)},
			{int16(x2 - minX), int16(y2 - minY)},
		},
	)

	// マスクを適用
	shape.Init(r.xConn)
	shape.Mask(
		r.xConn,
		shape.SoSet,
		shape.SkBounding,
		xproto.Window(win.Id),
		0, 0,
		maskPixmap,
	)

	xproto.FreeGC(r.xConn, maskGC)
	xproto.FreePixmap(r.xConn, maskPixmap)

	r.setupWindowClickThrough(win)
	r.xConn.Sync()

	segment := &TrailSegment{
		x1: x1, y1: y1, x2: x2, y2: y2,
		timestamp: time.Now(),
		window:    win,
		gc:        gc,
	}

	r.trails = append(r.trails, segment)
}

// setupWindowClickThrough 単一ウィンドウのクリックスルーを設定
func (r *Ruler) setupWindowClickThrough(win *xwindow.Window) {
	region, err := xfixes.NewRegionId(r.xConn)
	if err != nil {
		return
	}
	defer xfixes.DestroyRegion(r.xConn, region)

	xfixes.CreateRegionChecked(r.xConn, region, []xproto.Rectangle{{}}).Check()

	winID := xproto.Window(win.Id)
	xfixes.SetWindowShapeRegionChecked(r.xConn, winID, shape.SkInput, 0, 0, region).Check()
}

// updateTrails 軌跡の透明度を更新し、古い軌跡を削除
func (r *Ruler) updateTrails() {
	now := time.Now()
	atom, err := xproto.InternAtom(r.xConn, true, uint16(len(atomOpacity)), atomOpacity).Reply()
	if err != nil {
		return
	}

	newTrails := make([]*TrailSegment, 0, len(r.trails))

	for _, segment := range r.trails {
		elapsed := now.Sub(segment.timestamp)

		if elapsed > TrailDuration {
			segment.window.Destroy()
			xproto.FreeGC(r.xConn, segment.gc)
			continue
		}

		// 時間経過に応じて透明度を計算（新しい=100%, 古い=0%）
		progress := elapsed.Seconds() / TrailDuration.Seconds()
		opacityPercent := (1.0 - progress) * 100.0

		maxOpacity := float64(uint32(0xFFFFFFFF))
		opacity := opacityPercent / 100.0 * maxOpacity
		opacityValue := uint32(opacity)

		opacityBytes := []byte{
			byte(opacityValue & 0xFF),
			byte((opacityValue >> 8) & 0xFF),
			byte((opacityValue >> 16) & 0xFF),
			byte((opacityValue >> 24) & 0xFF),
		}

		winID := xproto.Window(segment.window.Id)
		xproto.ChangePropertyChecked(
			r.xConn,
			xproto.PropModeReplace,
			winID,
			atom.Atom,
			xproto.AtomCardinal,
			32,
			1,
			opacityBytes,
		).Check()

		newTrails = append(newTrails, segment)
	}

	r.trails = newTrails
	r.xConn.Sync()
}

