package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/charludo/imagepull-eval/client"
	"github.com/shirou/gopsutil/v3/disk"
)

var imageList = []string{
	"ghcr.io/edgelesssys/contrast/dmesg:v0.0.1@sha256:6ad6bbb5735b84b10af42d2441e8d686b1d9a6cbf096b53842711ef5ddabd28d",
	"ghcr.io/charludo/contrast/coordinator@sha256:6f966a922cc9a39d7047ed41ffafc7eb7a3c6a4fd8966cbf30fa902b455789f7",
	"quay.io/quay/busybox@sha256:92f3298bf80a1ba949140d77987f5de081f010337880cd771f7e7fc928f8c74d",
	"ghcr.io/edgelesssys/nginx-unprivileged@sha256:1d5be2aa3c296bd589ddd3c9bf2f560919e31ac32bae799a15dd182b6fdb042b",
	"quay.io/prometheus/prometheus@sha256:f20d3127bf2876f4a1df76246fca576b41ddf1125ed1c546fbd8b16ea55117e6",
	"ghcr.io/charludo/contrast/initializer@sha256:25b5ff1bd5259b6bd8c112b2321b8dc1857a9e63e0f2698c7ed4929c71ae514d",

	// Large number of layers, and large file size, respectively. Make sure to adjust the client timeout!
	// "tensorflow/tensorflow:latest-gpu@sha256:73fe35b67dad5fa5ab0824ed7efeb586820317566a705dff76142f8949ffcaff",
	// "floriangeigl/datascience@sha256:7bd3d9827056abfd87ef089a18ac3815e2e1e0ea360cf429cc8c6060788c8050",
}
var mountPoint = "current_server"

func getDiskUsage(path string) (uint64, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	return usage.Used, nil
}

func extractName(name string) string {
	at := strings.Index(name, "@")
	if at == -1 {
		return ""
	}
	slash := strings.LastIndex(name[:at], "/")
	return name[slash+1 : at]
}

func cleanup(storagePath string) {
	cmd := exec.Command("findmnt", "-rn", "-o", "TARGET")
	output, err := cmd.Output()
	if err != nil {
		log.Fatalf("failed to run findmnt: %v", err)
	}

	var mountpoints []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, storagePath) {
			mountpoints = append(mountpoints, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("error reading findmnt output: %v", err)
	}

	sort.Slice(mountpoints, func(i, j int) bool {
		return strings.Count(mountpoints[i], "/") > strings.Count(mountpoints[j], "/")
	})

	for _, mp := range mountpoints {
		if err := unix.Unmount(mp, 0); err != nil {
			log.Fatalf("Failed to unmount %s: %v", mp, err)
		}
	}

	if err := os.RemoveAll(storagePath); err != nil {
		log.Fatalf("Failed to remove directory %s: %v", storagePath, err)
	}
}

func findChildPid(ppid int) (int, error) {
	out, err := exec.Command("ps", "-o", "pid=", "--ppid", fmt.Sprint(ppid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps failed: %w", err)
	}
	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		return pid, nil
	}
	return 0, fmt.Errorf("no child found for PID %d", ppid)
}

func startServerWithMemoryTracking(serverPath, args string) (func() (int, error), int, error) {
	var stderr bytes.Buffer
	var cmd *exec.Cmd

	timeCmd := exec.Command("which", "time")
	out, err := timeCmd.Output()
	if err != nil {
		return nil, 0, fmt.Errorf("Error finding time bin: %w", err)
	}
	timePath := strings.TrimSpace(string(out))

	if len(args) == 0 {
		cmd = exec.Command(timePath, "-v", serverPath)
	} else {
		cmd = exec.Command(timePath, "-v", serverPath, args)
	}
	cmd.Stdout = nil
	cmd.Stderr = &stderr

	err = cmd.Start()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to start server: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	childPid, err := findChildPid(cmd.Process.Pid)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find server child PID: %w", err)
	}

	// Closure that will wait and extract MaxRSS after process exit
	waitAndGetMaxRSS := func() (int, error) {
		cmd.Wait()
		if err != nil {
			return 0, fmt.Errorf("command exited with error: %w", err)
		}

		scanner := bufio.NewScanner(&stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Maximum resident set size (kbytes)") {
				parts := strings.Fields(line)
				if len(parts) >= 6 {
					kb, err := strconv.Atoi(parts[len(parts)-1])
					if err != nil {
						return 0, fmt.Errorf("parsing MaxRSS failed: %w", err)
					}
					return kb, nil
				}
			}
		}
		return 0, fmt.Errorf("MaxRSS not found in output")
	}

	return waitAndGetMaxRSS, childPid, nil
}

func profileServerIndividual(serverPath, args, storagePath string, label string) {
	fmt.Printf("===== Testing server (individual): %s =====\n", label)
	defer cleanup(storagePath)
	defer cleanup(mountPoint)

	for _, image := range imageList {
		cleanup(storagePath)
		cleanup(mountPoint)
		fmt.Printf("[%s]\n", extractName(image))

		waitForRSS, childPid, err := startServerWithMemoryTracking(serverPath, args)
		if err != nil {
			log.Fatalf("Failed to start server %s: %v", label, err)
		}
		time.Sleep(500 * time.Millisecond)

		diskBefore, _ := getDiskUsage(storagePath)

		start := time.Now()
		err = client.Request(image, mountPoint)
		if err != nil {
			log.Printf("Error pulling image %s: %v", image, err)
		}

		duration := time.Since(start)
		syscall.Kill(childPid, syscall.SIGKILL)
		diskAfter, _ := getDiskUsage(storagePath)
		maxRSSkb, err := waitForRSS()
		if err != nil {
			log.Printf("Warning: could not get MaxRSS: %v", err)
		}

		fmt.Printf("Time taken: %s\n", duration.Round(10*time.Millisecond))
		fmt.Printf("Memory peak: %.2f MB\n", float64(maxRSSkb)/1024)
		fmt.Printf("Storage used: %.2f MB\n", float64(diskAfter-diskBefore)/1024/1024)
		fmt.Println()
	}
}

func profileServerContinuous(serverPath, args, storagePath string, label string) {
	fmt.Printf("===== Testing server (continuous): %s =====\n", label)
	cleanup(storagePath)
	defer cleanup(storagePath)
	defer cleanup(mountPoint)

	waitForRSS, childPid, err := startServerWithMemoryTracking(serverPath, args)
	if err != nil {
		log.Fatalf("Failed to start server %s: %v", label, err)
	}
	time.Sleep(500 * time.Millisecond)

	diskBefore, _ := getDiskUsage(storagePath)

	start := time.Now()
	for _, image := range imageList {
		cleanup(mountPoint)
		err = client.Request(image, mountPoint)
		if err != nil {
			log.Printf("Error pulling image %s: %v", image, err)
		}
	}
	duration := time.Since(start)

	syscall.Kill(childPid, syscall.SIGKILL)
	diskAfter, _ := getDiskUsage(storagePath)
	maxRSSkb, err := waitForRSS()
	if err != nil {
		log.Printf("Warning: could not get MaxRSS: %v", err)
	}

	fmt.Printf("Time taken: %s\n", duration.Round(10*time.Millisecond))
	fmt.Printf("Memory peak: %.2f MB\n", float64(maxRSSkb)/1024)
	fmt.Printf("Storage used: %.2f MB\n", float64(diskAfter-diskBefore)/1024/1024)
	fmt.Println()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: go run main.go /path/to/imagepuller /path/to/image-rs")
		return
	}

	imagepuller := os.Args[1]
	imagers := os.Args[2]

	// Dirs to clean up after each pull
	imagepullerDir := "tmp_imagepuller"
	imagersDir := "/run/kata-containers"

	profileServerIndividual(imagers, "", imagersDir, "image-rs")
	profileServerIndividual(imagepuller, fmt.Sprintf("--tmpdir=%s", imagepullerDir), imagepullerDir, "imagepuller")

	// profileServerContinuous(imagepuller, fmt.Sprintf("--tmpdir=%s", imagepullerDir), imagepullerDir, "imagepuller")
	// profileServerContinuous(imagers, "", imagersDir, "image-rs")
}
