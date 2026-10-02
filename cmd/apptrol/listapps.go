package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Dzobash/apptrol/internal/launcher"
)

// cmdListApps shows the installed apps with their desktop IDs, the names a
// launcher uses (`app = "<desktop ID>"`), filtered by search (LAUNCH-09).
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
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DESKTOP ID\tNAME\tSOURCE")
	for _, a := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", a.ID, a.Name, a.Source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "\nUse the desktop ID in a launcher, e.g. record = { app = \""+list[0].ID+"\" }")
	return nil
}
