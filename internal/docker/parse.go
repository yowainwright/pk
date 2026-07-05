package docker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

type containerRow struct {
	ID      string
	Image   string
	Names   string
	Command string
	Labels  string
}

func parseContainers(output []byte) ([]Container, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	containers := make([]Container, 0)
	for scanner.Scan() {
		container, ok, err := parseContainerLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		if ok {
			containers = append(containers, container)
		}
	}
	return containers, scanner.Err()
}

func parseContainerLine(line []byte) (Container, bool, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return Container{}, false, nil
	}
	var row containerRow
	if err := json.Unmarshal(line, &row); err != nil {
		return Container{}, false, err
	}
	return row.container(), true, nil
}

func (r containerRow) container() Container {
	return Container{
		ID:      r.ID,
		Name:    r.Names,
		Image:   r.Image,
		Command: r.Command,
		Labels:  parseLabels(r.Labels),
	}
}

func parseLabels(value string) map[string]string {
	labels := make(map[string]string)
	for _, part := range strings.Split(value, ",") {
		key, labelValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key != "" {
			labels[key] = labelValue
		}
	}
	return labels
}
