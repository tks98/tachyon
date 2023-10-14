package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
)

var (
	// Mutex for safe access to the cache
	cacheMutex sync.RWMutex
	// Cache for container data
	containerCache = make(map[string]Container)
	// Timestamp for the last time the data was refreshed
	lastRefreshed time.Time
)

type Container struct {
	OciVersion       string            `json:"ociVersion"`
	ID               string            `json:"id"`
	PID              int               `json:"pid"`
	Status           string            `json:"status"`
	Bundle           string            `json:"bundle"`
	RootFS           string            `json:"rootfs"`
	Created          string            `json:"created"`
	Annotations      map[string]string `json:"annotations"`
	Owner            string            `json:"owner"`
	OpenFiles        []LsofOutput      `json:"open_files"`
	NetworkUsage     NetworkUsage      `json:"network_usage"`
	MountedVolumes   []string          `json:"mounted_volumes"`
	ExposedPorts     []int             `json:"exposed_ports"`
	TopProcesses     []ProcessInfo     `json:"top_processes"`
	SecurityProfiles []string          `json:"security_profiles"`
	StartCommand     string            `json:"start_command"`
	ResourceLimits   ResourceLimits    `json:"resource_limits"`
	EnvVariables     []string
	ResourceUsage    ResourceUsage
}

type NetworkUsage struct {
	ReceivedBytes    int `json:"received_bytes"`
	TransmittedBytes int `json:"transmitted_bytes"`
}

type ProcessInfo struct {
	PID  int    `json:"pid"`
	User string `json:"user"`
	CPU  float64
	MEM  float64
	CMD  string `json:"cmd"`
}

type LsofOutput struct {
	Command string `json:"command"`
	PID     string `json:"pid"`
	User    string `json:"user"`
	FD      string `json:"fd"`
	Type    string `json:"type"`
	Device  string `json:"device"`
	SizeOff string `json:"size_off"`
	Node    string `json:"node"`
	Name    string `json:"name"`
}

type ResourceLimits struct {
	CPULimit     float64 // in percentage or cores
	MemoryLimit  int     // in kB
	DiskIOLimit  int     // in IOPS or MB/s
	NetworkLimit int     // in MB/s
}

type ResourceUsage struct {
	CPUUsage    float64        // in percentage
	MemoryUsage map[string]int // in kB
	SwapUsage   int
}

func StartCacheRefresh() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				containers, err := GetContainers(true) // Refresh the cache
				if err == nil {
					cacheMutex.Lock()
					for _, container := range containers {
						containerCache[strconv.Itoa(container.PID)] = container
					}
					lastRefreshed = time.Now()
					cacheMutex.Unlock()
				}
			}
		}
	}()
}

// GetContainerByID retrieves container information by its ID.
func GetContainerByID(pid string) (Container, error) {
	// Read-lock the cache to ensure safe access
	cacheMutex.RLock()
	container, exists := containerCache[pid]
	cacheMutex.RUnlock()

	// Check if the container data exists and if the cache is still fresh
	if exists && time.Since(lastRefreshed) < 20*time.Second {
		return container, nil
	}

	pidInt, err := strconv.Atoi(pid)
	if err != nil {
		return Container{}, fmt.Errorf("invalid PID format: %w", err)
	}

	containers, err := GetContainers(false)
	if err != nil {
		return Container{}, err
	}

	// Search for the container by PID and populate it if found
	for _, container := range containers {
		if container.PID == pidInt {
			err := container.PopulateContainer()
			if err != nil {
				return Container{}, fmt.Errorf("failed to populate container: %w", err)
			}
			return container, nil
		}
	}

	// Update the cache and the timestamp with the new data
	cacheMutex.Lock()
	containerCache[pid] = container
	lastRefreshed = time.Now()
	cacheMutex.Unlock()

	return container, nil
}

// GetContainers retrieves a list of containers.
func GetContainers(populate bool) ([]Container, error) {
	// Check if the cache is still fresh
	if time.Since(lastRefreshed) < 10*time.Second {
		cacheMutex.RLock()
		cachedContainers := make([]Container, 0, len(containerCache))
		for _, container := range containerCache {
			cachedContainers = append(cachedContainers, container)
		}
		cacheMutex.RUnlock()
		return cachedContainers, nil
	}

	// Execute runc to fetch information about running containers
	out, err := exec.Command("sudo", "runc", "--root", "/run/containerd/runc/k8s.io", "list", "--format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("error executing runc command: %w", err)
	}

	if len(out) == 0 {
		return nil, errors.New("runc output is empty")
	}

	var containers []Container
	err = json.Unmarshal(out, &containers)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal the runc output: %w", err)
	}

	// Optionally populate container data
	if populate {
		for i, _ := range containers {
			err := containers[i].PopulateContainer()
			if err != nil {
				return nil, err
			}
		}
	}

	// Update the cache and the timestamp with the new data
	cacheMutex.Lock()
	containerCache = make(map[string]Container)
	for _, container := range containers {
		containerCache[strconv.Itoa(container.PID)] = container
	}
	lastRefreshed = time.Now()
	cacheMutex.Unlock()

	return containers, nil
}

// PopulateContainer retrieves information about the calling container by PID
func (c *Container) PopulateContainer() error {
	var err error

	c.OpenFiles, err = c.getOpenFiles()
	if err != nil {
		return fmt.Errorf("failed to get open files: %w", err)
	}

	c.NetworkUsage, err = c.getContainerNetworkUsage()
	if err != nil {
		return fmt.Errorf("failed to get network usage: %w", err)
	}

	c.MountedVolumes, err = c.getContainerMountedVolumes()
	if err != nil {
		return fmt.Errorf("failed to get mounted volumes: %w", err)
	}

	c.ExposedPorts, err = c.getContainerExposedPorts()
	if err != nil {
		return fmt.Errorf("failed to get exposed ports: %w", err)
	}

	c.StartCommand, err = c.getContainerStartCommand()
	if err != nil {
		return fmt.Errorf("failed to get start command: %w", err)
	}

	c.SecurityProfiles, err = c.getContainerSecurityProfiles()
	if err != nil {
		return fmt.Errorf("failed to get security profiles: %w", err)
	}

	c.EnvVariables, err = c.getEnvironmentVariables()
	if err != nil {
		return fmt.Errorf("failed to get environment variables: %w", err)
	}

	c.ResourceUsage, err = c.getContainerResourceUsage()
	if err != nil {
		return fmt.Errorf("failed to get resource usage: %w", err)
	}

	return nil
}

// getContainerResourceUsage retrieves resource usage information
// (CPU and memory) for the container.
func (c *Container) getContainerResourceUsage() (ResourceUsage, error) {
	cpuUsage, err := c.getContainerCPUUsage()
	if err != nil {
		return ResourceUsage{}, fmt.Errorf("error getting CPU usage: %w", err)
	}

	memoryUsage, err := c.getContainerMemoryDetails()
	if err != nil {
		return ResourceUsage{}, fmt.Errorf("error getting memory usage: %w", err)
	}

	return ResourceUsage{
		CPUUsage:    cpuUsage,
		MemoryUsage: memoryUsage,
	}, nil
}

// getContainerCPUUsage retrieves the CPU usage percentage for the container.
func (c *Container) getContainerCPUUsage() (float64, error) {
	p, err := process.NewProcess(int32(c.PID))
	if err != nil {
		return 0, err
	}

	cpuTimes, err := p.Times()
	if err != nil {
		return 0, err
	}

	cpuUsagePercentage := (cpuTimes.User + cpuTimes.System) * 100

	return cpuUsagePercentage, nil
}

// getEnvironmentVariables retrieves the environment variables for the container.
func (c *Container) getEnvironmentVariables() ([]string, error) {
	p, err := process.NewProcess(int32(c.PID))
	if err != nil {
		return nil, err
	}

	envsSlice, err := p.Environ()
	if err != nil {
		return nil, err
	}

	return envsSlice, nil
}

// getContainerExposedPorts retrieves the list of exposed ports for the container.
func (c *Container) getContainerExposedPorts() ([]int, error) {
	var ports []int
	stats, err := net.ConnectionsPid("inet", int32(c.PID))
	if err != nil {
		return nil, err
	}
	for _, stat := range stats {
		if stat.Status == "LISTEN" {
			ports = append(ports, int(stat.Laddr.Port))
		}
	}
	return ports, nil
}

// getContainerExposedPorts retrieves the list of exposed ports for the container.
func (c *Container) getContainerStartCommand() (string, error) {
	p, err := process.NewProcess(int32(c.PID))
	if err != nil {
		return "", err
	}

	cmd, err := p.Cmdline()
	if err != nil {
		return "", err
	}

	cmd = strings.TrimSpace(cmd)
	return cmd, nil
}

// getContainerExposedPorts retrieves the list of exposed ports for the container.
func (c *Container) getContainerSecurityProfiles() ([]string, error) {
	path := fmt.Sprintf("/proc/%d/attr/current", c.PID)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	profiles := strings.Split(string(content), ",")
	for i := range profiles {
		profiles[i] = strings.TrimSpace(profiles[i])
	}

	return profiles, nil
}

// getContainerSwapUsage retrieves the amount of swap memory used by the container.
func (c *Container) getContainerSwapUsage() (int, error) {
	proc, err := process.NewProcess(int32(c.PID))
	if err != nil {
		return 0, err
	}

	memoryInfo, err := proc.MemoryInfo()
	if err != nil {
		return 0, err
	}

	return int(memoryInfo.Swap), nil
}

// getContainerMemoryDetails retrieves detailed memory information (RSS and VMS)
// for the container.
func (c *Container) getContainerMemoryDetails() (map[string]int, error) {
	proc, err := process.NewProcess(int32(c.PID))
	if err != nil {
		return nil, err
	}

	memoryInfo, err := proc.MemoryInfo()
	if err != nil {
		return nil, err
	}

	memoryDetails := make(map[string]int)
	memoryDetails["RSS"] = int(memoryInfo.RSS)
	memoryDetails["VMS"] = int(memoryInfo.VMS)

	return memoryDetails, nil
}

// getContainerNetworkInterfaces retrieves network interfaces and their IP addresses
// associated with the container.
func (c *Container) getContainerNetworkInterfaces() (map[string]string, error) {
	info := make(map[string]string)
	out, err := exec.Command("sudo", "nsenter", "-t", fmt.Sprint(c.PID), "-n", "ifconfig").Output()

	if err != nil {
		return nil, err
	}

	output := strings.Split(string(out), "\n")

	for _, line := range output {
		if strings.Contains(line, "inet ") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				info[fields[0]] = fields[1]
			}
		}
	}

	return info, nil
}

// getOpenFiles retrieves a list of open files associated with the container.
func (c *Container) getOpenFiles() ([]LsofOutput, error) {
	cmd := exec.Command("sudo", "lsof", "-F", "-n", "-p", strconv.Itoa(c.PID))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	var entries []LsofOutput
	var entry LsofOutput
	for _, line := range lines {
		if len(line) > 1 {
			switch line[0] {
			case 'p':
				if entry.PID != "" {
					entries = append(entries, entry)
					entry = LsofOutput{}
				}
				entry.PID = line[1:]
			case 'c':
				entry.Command = line[1:]
			case 'u':
				entry.User = line[1:]
			case 'f':
				entry.FD = line[1:]
			case 't':
				entry.Type = line[1:]
			case 'D':
				entry.Device = line[1:]
			case 's':
				entry.SizeOff = line[1:]
			case 'i':
				entry.Node = line[1:]
			case 'n':
				entry.Name = line[1:]
			}
		}
	}
	if entry.PID != "" {
		entries = append(entries, entry)
	}

	return entries, nil
}

// getContainerNetworkUsage retrieves network usage statistics (received and transmitted bytes)
// for the container.
func (c *Container) getContainerNetworkUsage() (NetworkUsage, error) {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/dev", c.PID))
	if err != nil {
		return NetworkUsage{}, err
	}

	lines := strings.Split(string(content), "\n")
	totalReceivedBytes := 0
	totalTransmittedBytes := 0

	for _, line := range lines {
		fields := strings.Fields(line)

		if len(fields) > 10 {
			receivedBytes, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}

			transmittedBytes, err := strconv.Atoi(fields[9])
			if err != nil {
				continue
			}

			totalReceivedBytes += receivedBytes
			totalTransmittedBytes += transmittedBytes
		}
	}

	return NetworkUsage{
		ReceivedBytes:    totalReceivedBytes,
		TransmittedBytes: totalTransmittedBytes,
	}, nil
}

// getContainerMountedVolumes retrieves a list of mounted volumes within the container.
func (c *Container) getContainerMountedVolumes() ([]string, error) {
	volumes := []string{}

	filePath := fmt.Sprintf("/proc/%d/mounts", c.PID)
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(fileContent), "\n")
	for _, line := range lines {
		parts := strings.Split(line, " ")
		if len(parts) > 2 {
			volumes = append(volumes, parts[1])
		}
	}

	return volumes, nil
}
