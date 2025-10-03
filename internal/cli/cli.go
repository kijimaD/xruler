package cli

import (
	"context"

	"github.com/kijimaD/xruler/internal/ruler"
	"github.com/urfave/cli/v3"
)

// NewCommand は xruler の CLI コマンドを作成する
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "xruler",
		Usage: "X Window System上でカーソル位置を追従する水平ルーラー",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "mode",
				Aliases: []string{"m"},
				Value:   "ruler",
				Usage:   "動作モード: `MODE` (ruler または hide)",
			},
		},
		Action: run,
	}
}

// run は CLI コマンドのアクション関数
func run(ctx context.Context, cmd *cli.Command) error {
	modeStr := cmd.String("mode")

	var mode ruler.Mode
	switch modeStr {
	case "hide":
		mode = ruler.DefaultHideModeConfig()
	case "ruler":
		mode = ruler.DefaultRulerModeConfig()
	default:
		return cli.Exit("Error: Invalid mode '"+modeStr+"'. Use 'hide' or 'ruler'.", 1)
	}

	r := ruler.New(mode)
	defer r.Close()

	if err := r.Init(); err != nil {
		return err
	}

	r.Run()
	return nil
}
