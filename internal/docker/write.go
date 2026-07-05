package docker

import (
	"fmt"
	"io"
	"strings"
)

func WriteResults(w io.Writer, results []Result) error {
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "CONTAINER\tAPPLIED\tNAME\tIMAGE\tERROR\tREASONS"); err != nil {
		return err
	}
	return writeRows(w, results)
}

func writeRows(w io.Writer, results []Result) error {
	for _, result := range results {
		if err := writeRow(w, result); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, result Result) error {
	container := result.Report.Container
	_, err := fmt.Fprintf(w, "%s\t%t\t%s\t%s\t%s\t%s\n",
		container.ID,
		result.Applied,
		container.Name,
		container.Image,
		result.Error,
		strings.Join(result.Report.Reasons, ","),
	)
	return err
}
