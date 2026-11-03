package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Dzobash/apptrol/internal/launcher"
)

// cmdListApps shows the installed apps with their desktop IDs, the names a
// launcher uses (`app = "<desktop ID>"`), filtered by search (LAUNCH-09).
// Two lines above the table say these IDs are for launchers, not the names
// `match` uses: those come from `apptrol list` while the app plays.
func cmdListApps(stdout io.Writer, search string, apps launcher.Apps) error {
	list := apps.Search(search)
	if len(list) == 0 {
		if search != "" {
			fmt.Fprintf(stdout, "No installed app matches %q.\n", search)
		} else {
			fmt.Fprintln(stdout, "No installed apps found.")
		}
		return nil
	}
	// Above the table, so a list of desktop IDs is not taken for the names
	// `match` needs (LAUNCH-09).
	fmt.Fprintln(stdout, `Installed apps, for launchers (app = "<desktop ID>").`)
	fmt.Fprintln(stdout, "For match = […] in [apps], the name can differ: run `apptrol list` while the app plays.")
	fmt.Fprintln(stdout)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DESKTOP ID\tNAME\tSOURCE")
	for _, a := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", a.ID, a.Name, a.Source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// Without a search word the first ID is whichever app sorts first. An ID
	// can contain '=', and `record = { app = "…" }` then reads like a syntax
	// error. A search word narrowed the list, so that first ID is the one
	// the person asked about (LAUNCH-09).
	example := "<desktop ID>"
	if search != "" {
		example = list[0].ID
	}
	fmt.Fprintln(stdout, "\nUse the desktop ID in a launcher, e.g. record = { app = \""+example+"\" }")
	return nil
}
