package main

import (
	"flag"
	"fmt"
	"github.com/evilsocket/uroboros/host"
	"github.com/evilsocket/uroboros/record"
	ui "github.com/gizak/termui/v3"
	"github.com/prometheus/procfs"
	"os"
	"runtime/pprof"
	"sort"
	"strings"
	"time"
)

var err error

var cpuProfile = ""
var targetName = ""
var refreshPeriod = 500

func init() {
	flag.IntVar(&host.TargetPID, "pid", 0, "Process ID to monitor.")
	flag.StringVar(&targetName, "search", "", "Search target process by name.")
	flag.IntVar(&refreshPeriod, "period", refreshPeriod, "Data refresh period in milliseconds.")
	flag.StringVar(&host.ProcFS, "procfs", host.ProcFS, "Root of the proc filesystem.")
	flag.StringVar(&tabIDS, "tabs", tabIDS, "Comma separated list of tab names to show.")

	flag.StringVar(&recordFile, "record", recordFile, "If specified, record the session to this file.")
	flag.StringVar(&replayFile, "replay", replayFile, "If specified, replay the session in this file.")

	flag.StringVar(&cpuProfile, "cpu-profile", cpuProfile, "Used for debugging.")
}

func searchTarget() {
	if targetName != "" {
		if procs, err := procfs.AllProcs(); err != nil {
			panic(err)
		} else {
			matchPIDs := make([]int, 0)
			matches := make(map[int]procfs.Proc)
			for _, proc := range procs {
				if comm, _ := proc.Comm(); comm != "" && strings.Index(comm, targetName) != -1 {
					matches[proc.PID] = proc
					matchPIDs = append(matchPIDs, proc.PID)
				}
			}

			if num := len(matches); num == 0 {
				fmt.Printf("no matches for '%s'\n", targetName)
				os.Exit(1)
			} else if num > 1 {
				fmt.Printf("multiple matches for '%s':\n", targetName)

				sort.Ints(matchPIDs)

				for _, pid := range matchPIDs {
					proc := matches[pid]
					comm, _ := proc.Comm()
					cmdline, _ := proc.CmdLine()
					fmt.Printf("[%d] (%s) %s\n", pid, comm, strings.Join(cmdline, " "))
				}
				os.Exit(0)
			} else {
				for pid := range matches {
					host.TargetPID = pid
					return
				}
			}
		}
	}

	if host.TargetPID <= 0 {
		host.TargetPID = os.Getpid()
	}
}

func main() {
	flag.Parse()

	searchTarget()

	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if recordFile != "" {
		if recorder, err = record.New(); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	} else if replayFile != "" {
		if player, err = record.Load(replayFile); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	}

	if err = setupUI(host.TargetPID); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	// most tabs need at least two data points to correctly render
	for i := 0; i < 2; i++ {
		updateTabs()
	}

	updateUI()

	defer closeUI()

	uiEvents := ui.PollEvents()
	ticker := time.NewTicker(time.Millisecond * time.Duration(refreshPeriod)).C
	for {
		select {
		case <-ticker:
			updateTabs()

		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return

			case "<Resize>":
				payload := e.Payload.(ui.Resize)
				grid.SetRect(0, 0, payload.Width, payload.Height)

			case "<Left>":
				tabs.FocusLeft()
			case "<Right>":
				tabs.FocusRight()

			case "<Space>", "p":
				paused = !paused

			case "f":
				updateUI()
				updateTabs()
			}

			// propagate to current view
			getActiveTab().Event(e)
		}

			updateUI()
	}
}
