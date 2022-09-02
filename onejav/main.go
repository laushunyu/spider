package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"runtime"
)

// configs
var config = struct {
	Concurrent int
	Limit      int
	Output     string
}{}

var root = cobra.Command{
	Use: "onejav",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return os.MkdirAll(config.Output, os.ModePerm)
	},
}

func init() {
	root.PersistentFlags().IntVarP(&config.Concurrent, "concurrent", "j", runtime.GOMAXPROCS(0), "concurrent goroutine to download")
	root.PersistentFlags().IntVarP(&config.Limit, "limit", "c", 0, "count limit of artifact")
	root.PersistentFlags().StringVarP(&config.Output, "output", "o", ".", "output dir")
	root.AddCommand(cmdDownloadByUrl)
}

func main() {
	if err := root.Execute(); err != nil {
		log.Error(err)
	}
}
