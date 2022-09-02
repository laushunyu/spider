package main

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sync"
)

var cmdDownloadByUrl = &cobra.Command{
	Use:   "url list_url",
	Short: "get all artifacts from this list page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		count := 0
		wg := sync.WaitGroup{}
		ch := make(chan *Artifact, config.Concurrent)
		for i := 0; i < config.Concurrent; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for art := range ch {
					if err := art.DownloadTo(config.Output); err != nil {
						log.WithError(err).Warnf("failed to download %s, skip", art.ID)
					}
				}
			}()
		}

		list, err := GetListPage(cmd.Context(), args[0])
		if err != nil {
			return err
		}

	all:
		for {
			arts, err := list.Artifacts()
			if err != nil {
				return err
			}
			if len(arts) == 0 {
				break
			}
			for i := range arts {
				count++
				if count > config.Limit {
					break all
				}
				ch <- &arts[i]
			}

			list, err = list.Next(ctx)
			if err != nil {
				if errors.Is(err, ErrorIsLastPage) {
					break
				}
				return err
			}
		}

		close(ch)
		wg.Wait()
		return nil
	},
}
