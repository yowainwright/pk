package docker

import (
	"sort"
	"strings"
)

const (
	ActionStop     = "stop"
	ConfidenceHigh = "high"
)

func reportForContainer(container Container) (Report, bool) {
	if protected(container) {
		return Report{}, false
	}
	reasons := reasonsForContainer(container)
	if len(reasons) == 0 {
		return Report{}, false
	}
	return Report{
		Container:  container,
		Action:     ActionStop,
		Confidence: ConfidenceHigh,
		Reasons:    reasons,
	}, true
}

func reasonsForContainer(container Container) []string {
	reasons := make([]string, 0, 2)
	if hasComposeLabels(container.Labels) {
		reasons = append(reasons, "compose-container")
	}
	if hasDevContainerLabels(container.Labels) {
		reasons = append(reasons, "devcontainer")
	}
	if hasLocalWorkdir(container.Labels) {
		reasons = append(reasons, "local-workdir")
	}
	return reasons
}

func protected(container Container) bool {
	return container.Labels["pk.protected"] == "true"
}

func hasComposeLabels(labels map[string]string) bool {
	_, hasProject := labels["com.docker.compose.project"]
	_, hasWorkingDir := labels["com.docker.compose.project.working_dir"]
	return hasProject || hasWorkingDir
}

func hasDevContainerLabels(labels map[string]string) bool {
	for key := range labels {
		if strings.Contains(strings.ToLower(key), "devcontainer") {
			return true
		}
	}
	return false
}

func hasLocalWorkdir(labels map[string]string) bool {
	workdir := labels["com.docker.compose.project.working_dir"]
	if workdir == "" {
		workdir = labels["devcontainer.local_folder"]
	}
	return isLocalPath(workdir)
}

func isLocalPath(path string) bool {
	return strings.HasPrefix(path, "/Users/") || strings.HasPrefix(path, "/home/")
}

func sortReports(reports []Report) {
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Container.ID < reports[j].Container.ID
	})
}
