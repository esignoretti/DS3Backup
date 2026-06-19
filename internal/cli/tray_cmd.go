package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/esignoretti/ds3backup/internal/tray"
)

var trayPort int

// trayCmd represents the `ds3backup tray` command.
var trayCmd = &cobra.Command{
	Use:   "tray",
	Short: "Manage the system tray",
	Long:  `Start and manage the system tray application (macOS only).`,
}

// trayStartCmd represents the `ds3backup tray start` command.
// This is the entry point called by the daemon's tray subprocess.
var trayStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the system tray",
	Long: `Starts the system tray application, connecting to the daemon API on the given port.
This command is intended to be invoked by the daemon as a subprocess.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if trayPort == 0 {
			return fmt.Errorf("--port is required")
		}

		trayApp := tray.NewTrayApp(trayPort)

		// Forward SIGINT/SIGTERM to systray.Quit() for clean shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigChan
			trayApp.Stop()
		}()

		trayApp.RunBlocking()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(trayCmd)
	trayCmd.AddCommand(trayStartCmd)

	trayStartCmd.Flags().IntVar(&trayPort, "port", 0, "Daemon API port to connect to")
}
