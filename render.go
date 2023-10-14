package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// createAppTable creates and configures a table widget for displaying container information.
func createAppTable(app *tview.Application) *tview.Table {
	table := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false). // Make rows selectable, not columns
		SetFixed(1, 0).             // Fix the first row (headers)
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEscape {
				app.Stop()
			}
		})

	table.SetBackgroundColor(tcell.ColorBlack).SetBorder(true).SetTitle(" Containers List ").SetBorderPadding(0, 0, 1, 1)

	return table
}

// createDetailsTextview creates and configures a text view widget for displaying container details.
func createDetailsTextview(app *tview.Application, table *tview.Table) *tview.TextView {
	textView := tview.NewTextView().SetDynamicColors(true)
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyLeft:
			app.SetFocus(table)
		}
		return event
	})

	textView.SetBackgroundColor(tcell.ColorBlack).SetBorder(true).SetTitle(" Container Details ").SetBorderPadding(0, 0, 1, 1)

	return textView
}

// createDetailsTextview creates and configures a text view widget for displaying container details.
func refreshTable(table *tview.Table, detailsTextView *tview.TextView) {
	containers, err := GetContainers(true)
	if err != nil {
		panic(err)
	}

	table.Clear()

	// Table headers
	table.SetCell(0, 0, tview.NewTableCell("PID").SetAlign(tview.AlignCenter))
	table.SetCell(0, 1, tview.NewTableCell("Owner").SetAlign(tview.AlignCenter))
	table.SetCell(0, 2, tview.NewTableCell("Created").SetAlign(tview.AlignCenter))
	table.SetCell(0, 3, tview.NewTableCell("Status").SetAlign(tview.AlignCenter))

	layout := "2006-01-02T15:04:05.999999999Z"
	for i, container := range containers {
		t, err := time.Parse(layout, container.Created)
		if err != nil {
			panic(err)
		}
		formatted := t.Format("02-Jan-2006-03:04 PM")
		table.SetCell(i+1, 0, tview.NewTableCell(strconv.Itoa(container.PID)).SetAlign(tview.AlignCenter))
		table.SetCell(i+1, 1, tview.NewTableCell(container.Owner).SetAlign(tview.AlignCenter))
		table.SetCell(i+1, 2, tview.NewTableCell(formatted).SetAlign(tview.AlignCenter))
		table.SetCell(i+1, 3, tview.NewTableCell(container.Status).SetAlign(tview.AlignCenter))
	}

}

func updateDetails(table *tview.Table, detailsTextView *tview.TextView) {
	row, _ := table.GetSelection()

	pid := table.GetCell(row, 0).Text
	cacheMutex.RLock()
	container, exists := containerCache[pid]
	cacheMutex.RUnlock()

	if exists {
		showDetails(container, detailsTextView)
	}
}

// showDetails displays detailed information about a container in a TextView.
func showDetails(container Container, detailsTextView *tview.TextView) {
	// Initialize an empty details string to accumulate information.
	var details strings.Builder

	// Add container information to the details.
	details.WriteString(showContainerInfo(container))

	// Add resource usage information to the details.
	details.WriteString(showResourceUsage(container))

	// Add network usage information to the details.
	details.WriteString(showNetworkUsage(container))

	// Add exposed ports information to the details.
	details.WriteString(showExposedPorts(container))

	// Add mounted volumes information to the details.
	details.WriteString(showMountedVolumes(container))

	// Add Kubernetes metadata information to the details.
	details.WriteString(showKubernetesMetadata(container))

	// Add open files information to the details.
	details.WriteString(showOpenFiles(container))

	// Add environment variables information to the details.
	details.WriteString(showEnvironmentVariables(container))

	// Add security profiles information to the details.
	details.WriteString(showSecurityProfiles(container))

	// Set the TextView's text to the accumulated details.
	detailsTextView.SetText(details.String())
}

// showContainerInfo displays information about the container.
func showContainerInfo(container Container) string {
	// Initialize details string with PID first
	details := fmt.Sprintf("[::b]=== Container Info ===[::-]\n[::b]Container PID:[::-] %d\n", container.PID)

	// Check if the "io.kubernetes.cri.image-name" key exists in annotations
	if imageName, ok := container.Annotations["io.kubernetes.cri.image-name"]; ok {
		details += fmt.Sprintf("[::b]Image Name:[::-] %s\n", imageName)
	}

	// Append the rest of the details
	details += fmt.Sprintf("[::b]ID:[::-] %s\n[::b]Status:[::-] %s\n[::b]Created:[::-] %s\n[::b]RootFS:[::-] %s\n[::b]CMD:[::-] %s\n",
		container.ID, container.Status, container.Created, container.RootFS, container.StartCommand)

	return details
}

// showResourceUsage displays resource usage information.
func showResourceUsage(container Container) string {
	details := "\n[::b]=== Resource Usage ===[::-]\n"
	details += fmt.Sprintf("[::b]CPU Usage:[::-] %.2f%%\n", container.ResourceUsage.CPUUsage)
	details += fmt.Sprintf("[::b]RSS Memory:[::-] %d kB\n[::b]VMS Memory:[::-] %d kB\n", container.ResourceUsage.MemoryUsage["RSS"], container.ResourceUsage.MemoryUsage["VMS"])
	details += fmt.Sprintf("[::b]Swap Usage:[::-] %d kB\n", container.ResourceUsage.SwapUsage)

	return details
}

// showNetworkUsage displays network usage information.
func showNetworkUsage(container Container) string {
	details := "\n[::b]=== Network Usage ===[::-]\n"
	details += fmt.Sprintf("[::b]Received Bytes:[::-] %d\n[::b]Transmitted Bytes:[::-] %d\n", container.NetworkUsage.ReceivedBytes, container.NetworkUsage.TransmittedBytes)

	return details
}

// showExposedPorts displays information about exposed ports.
func showExposedPorts(container Container) string {
	details := "\n[::b]=== Exposed Ports ===[::-]\n"

	for _, port := range container.ExposedPorts {
		details += fmt.Sprintf("[::b]Port:[::-] %d\n", port)
	}

	return details
}

// showMountedVolumes displays information about mounted volumes.
func showMountedVolumes(container Container) string {
	volumes := container.MountedVolumes

	details := "\n[::b]=== Mounted Volumes ===[::-]\n"

	// Display volumes in a table format
	for i := 0; i < len(volumes); i += 2 {
		// If there's a next volume, add it side by side
		if i+1 < len(volumes) {
			details += fmt.Sprintf("%-20s %-20s\n", volumes[i], volumes[i+1])
		} else {
			details += fmt.Sprintf("%-20s\n", volumes[i])
		}
	}

	return details
}

// showKubernetesMetadata displays Kubernetes metadata information.
func showKubernetesMetadata(container Container) string {
	details := "\n[::b]=== Kubernetes Metadata ===[::-]\n"

	// Extract keys from the map and sort them alphabetically
	var keys []string
	for key := range container.Annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Iterate over sorted keys and print annotations
	for _, key := range keys {
		value := container.Annotations[key]
		details += fmt.Sprintf("[::b]%s:[::-] %s\n", key, value)
	}

	return details
}

// showOpenFiles displays information about open files.
func showOpenFiles(container Container) string {
	details := "\n[::b]=== Open Files ===[::-]\n"
	for _, file := range container.OpenFiles {
		details += fmt.Sprintf("[::b]Command:[::-] %s, [::b]PID:[::-] %s, [::b]Name:[::-] %s\n", file.Command, file.PID, file.Name)
	}

	return details
}

// showEnvironmentVariables displays environment variables information.
func showEnvironmentVariables(container Container) string {
	details := "\n[::b]=== Environment Variables ===[::-]\n"

	for _, envVar := range container.EnvVariables {
		if strings.Contains(envVar, "=") {
			details += fmt.Sprintf("%s\n", envVar)
		}
	}

	return details
}

// showSecurityProfiles displays security profiles information.
func showSecurityProfiles(container Container) string {
	details := "\n[::b]=== Security Profiles ===[::-]\n"
	for _, profile := range container.SecurityProfiles {
		details += fmt.Sprintf("[::b]Profile:[::-] %s\n", profile)
	}

	return details
}
