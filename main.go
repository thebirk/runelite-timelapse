package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/schollz/progressbar/v3"
)

type Screenshot struct {
	Path string
	Time time.Time
}

func ByteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}

func waitInput() {
	fmt.Fprintf(os.Stderr, "Press any key to continue...")
	b := bufio.NewReader(os.Stdin)
	b.ReadString('\n')
}

func prompt(prompt string) string {
	os.Stderr.Write([]byte(prompt))
	b := bufio.NewReader(os.Stdin)
	r, _ := b.ReadString('\n')
	return strings.TrimSpace(r)
}

func TimelapseProfile(profile Profile) {
	framerate := prompt("Specify a framerate. Leave empty for a default of 5.\n> ")
	if framerate == "" {
		framerate = "5"
	}

	var screenshots []Screenshot

	var totalSize int64
	filepath.Walk(profile.Path, func(path string, info fs.FileInfo, err error) error {
		if filepath.Ext(path) != ".png" || info.IsDir() {
			return nil
		}

		screenshots = append(screenshots, Screenshot{
			Path: path,
			Time: info.ModTime(),
		})

		totalSize += info.Size()

		return nil
	})
	slices.SortFunc(screenshots, func(a Screenshot, b Screenshot) int {
		return a.Time.Compare(b.Time)
	})

	cmd := exec.Command("ffmpeg")
	cmd.Args = []string{
		"ffmpeg",
		"-f", "image2pipe",
		"-framerate", framerate,
		"-i", "-",
		"-s:v", "1920x1080",
		"-c:v", "libx264",
		"-vf", "format=yuv422p",
		"-r", "60",
		"-movflags", "+faststart",
		"-y", // overwrite output
		fmt.Sprintf("%s.mp4", profile.Name),
	}
	pipeIn, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open ffmpeg pipe: %s\n", err.Error())
		waitInput()
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Found %d screenshots. Total size: %s\n", len(screenshots), ByteCountIEC(totalSize))
	t1 := time.Now()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Start()

	skipped := 0
	bar := progressbar.Default(int64(len(screenshots)))
	for _, s := range screenshots {
		bytes, err := os.ReadFile(s.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read '%s', skipping. Error: %s\n", s.Path, err.Error())
			skipped += 1
			continue
		}
		bar.Add(1)
		_, _ = pipeIn.Write(bytes)
	}
	pipeIn.Close()

	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "ffmpeg error: %s\n%s\n", err.Error(), output.String())
		waitInput()
		os.Exit(1)
	}

	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Skipped %d screenshots. See output for info\n", skipped)
	}

	fmt.Fprintf(os.Stderr, "Finished in %s\n", time.Since(t1).String())
	waitInput()
}

type Profile struct {
	Name string
	Path string
}

func main() {
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find user home directory?: %s\n", err.Error())
		waitInput()
		os.Exit(1)
	}

	screenshots := filepath.Join(home, ".runelite", "screenshots")
	dirs, err := os.ReadDir(screenshots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open runelite screenshots folder: %s\n", err.Error())
		waitInput()
		os.Exit(1)
	}

	var profiles []Profile
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		profiles = append(profiles, Profile{
			Name: dir.Name(),
			Path: filepath.Join(screenshots, dir.Name()),
		})
	}

	fmt.Fprintf(os.Stderr, "Found the following profiles:\n")
	tw := tabwriter.NewWriter(os.Stderr, 0, 8, 2, ' ', 0)
	for i, profile := range profiles {
		fmt.Fprintf(tw, "\t%d\t%s\n", i+1, profile.Name)
	}
	tw.Flush()

	for {
		fmt.Fprintf(os.Stderr, "Type the number corresponding to the profile you want to timelapse.\n: ")
		reader := bufio.NewReader(os.Stdin)
		choiceString, _ := reader.ReadString('\n')

		choice, err := strconv.Atoi(strings.TrimSpace(choiceString))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Please input a number between %d and %d!\n", 1, len(profiles))
			continue
		}

		if choice <= 0 || choice > len(profiles) {
			fmt.Fprintf(os.Stderr, "Please input a number between %d and %d!\n", 1, len(profiles))
			continue
		}

		TimelapseProfile(profiles[choice-1])
		break
	}
}
