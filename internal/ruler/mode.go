package ruler

import (
	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xwindow"
)

// ModeType 動作モードの種類
type ModeType int

const (
	ModeTypeHide  ModeType = iota // 隠すモード（上下を暗くする）
	ModeTypeRuler                 // ルーラーモード（半透明の線を表示）
)

// Mode モードインターフェース
type Mode interface {
	// CreateWindows ウィンドウを作成
	CreateWindows(xuConn *xgbutil.XUtil, screenWidth, screenHeight int) ([]*xwindow.Window, error)
	// UpdateWindows カーソル位置に応じてウィンドウを更新
	UpdateWindows(xConn *xgb.Conn, windows []*xwindow.Window, cursorY, screenWidth, screenHeight int)
	// GetOpacity 不透明度を返す
	GetOpacity() float64
}
