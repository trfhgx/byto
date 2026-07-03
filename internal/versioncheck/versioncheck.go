package versioncheck

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPolicyURL = "https://raw.githubusercontent.com/trfhgx/vertex-gemini-openai-gateway/main/.github/version-policy.json"
	defaultVersion   = "0.1.0"
)

type Policy struct {
	Latest          string   `json:"latest"`
	DeprecatedBelow string   `json:"deprecated_below"`
	ObsoleteBelow   string   `json:"obsolete_below"`
	Deprecated      []string `json:"deprecated"`
	Obsolete        []string `json:"obsolete"`
	Message         string   `json:"message"`
	UpdateCommand   string   `json:"update_command"`
}

func Check(ctx context.Context) error {
	if truthy(os.Getenv("BYTO_SKIP_VERSION_CHECK")) || truthy(os.Getenv("SKIP_VERSION_CHECK")) {
		return nil
	}

	version := localVersion()
	policy, err := fetchPolicy(ctx, policyURL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not reach GitHub version policy; continuing with local version %s\n", version)
		return nil
	}

	if isObsolete(version, policy) {
		printObsolete(version, policy)
		if err := runUpdate(policy.UpdateCommand); err != nil {
			return err
		}
		return errors.New("Byto Gateway was updated; rerun the command to start the new version")
	}

	if isDeprecated(version, policy) {
		printDeprecated(version, policy)
		if shouldPrompt() && promptUpdate() {
			if err := runUpdate(policy.UpdateCommand); err != nil {
				return err
			}
			return errors.New("Byto Gateway was updated; rerun the command to start the new version")
		}
	}

	return nil
}

func policyURL() string {
	if v := os.Getenv("BYTO_VERSION_POLICY_URL"); v != "" {
		return v
	}
	return defaultPolicyURL
}

func localVersion() string {
	for _, path := range []string{"VERSION", "./VERSION"} {
		b, err := os.ReadFile(path)
		if err == nil {
			v := strings.TrimSpace(string(b))
			if v != "" {
				return v
			}
		}
	}
	return defaultVersion
}

func fetchPolicy(ctx context.Context, url string) (Policy, error) {
	if strings.HasPrefix(url, "file://") {
		b, err := os.ReadFile(strings.TrimPrefix(url, "file://"))
		if err != nil {
			return Policy{}, err
		}
		var p Policy
		err = json.Unmarshal(b, &p)
		return p, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Policy{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Policy{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Policy{}, fmt.Errorf("version policy returned %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(body, &p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func isObsolete(version string, p Policy) bool {
	return contains(p.Obsolete, version) || (p.ObsoleteBelow != "" && versionLess(version, p.ObsoleteBelow))
}

func isDeprecated(version string, p Policy) bool {
	return contains(p.Deprecated, version) || (p.DeprecatedBelow != "" && versionLess(version, p.DeprecatedBelow))
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func versionLess(left, right string) bool {
	l := versionParts(left)
	r := versionParts(right)
	if len(l) < len(r) {
		l = append(l, make([]int, len(r)-len(l))...)
	}
	if len(r) < len(l) {
		r = append(r, make([]int, len(l)-len(r))...)
	}
	for i := range l {
		if l[i] < r[i] {
			return true
		}
		if l[i] > r[i] {
			return false
		}
	}
	return false
}

var versionPartRE = regexp.MustCompile(`\d+`)

func versionParts(value string) []int {
	matches := versionPartRE.FindAllString(strings.TrimPrefix(value, "v"), -1)
	out := make([]int, 0, len(matches))
	for _, match := range matches {
		n, err := strconv.Atoi(match)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func printDeprecated(version string, p Policy) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr, "  THIS BYTO GATEWAY VERSION IS DEPRECATED")
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Installed: %s\n", version)
	if p.DeprecatedBelow != "" {
		fmt.Fprintf(os.Stderr, "Recommended: %s or newer\n", p.DeprecatedBelow)
	}
	if p.Latest != "" {
		fmt.Fprintf(os.Stderr, "Latest:    %s\n", p.Latest)
	}
	if p.Message != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", p.Message)
	}
	fmt.Fprintln(os.Stderr)
}

func printObsolete(version string, p Policy) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr, "  THIS BYTO GATEWAY VERSION IS OBSOLETE AND MUST BE UPDATED")
	fmt.Fprintln(os.Stderr, "================================================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Installed: %s\n", version)
	if contains(p.Obsolete, version) {
		fmt.Fprintln(os.Stderr, "Blocked:   this exact release is marked obsolete")
	} else if p.ObsoleteBelow != "" {
		fmt.Fprintf(os.Stderr, "Required:  %s or newer\n", p.ObsoleteBelow)
	}
	if p.Latest != "" {
		fmt.Fprintf(os.Stderr, "Latest:    %s\n", p.Latest)
	}
	if p.Message != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", p.Message)
	}
	fmt.Fprintln(os.Stderr)
}

func shouldPrompt() bool {
	if truthy(os.Getenv("NON_INTERACTIVE")) {
		return false
	}
	info, err := os.Stdin.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func promptUpdate() bool {
	fmt.Fprint(os.Stderr, "Update now? [Y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func runUpdate(updateCommand string) error {
	if updateCommand == "" {
		updateCommand = "git pull --ff-only origin main"
	}
	fmt.Fprintln(os.Stderr, "Updating Byto Gateway...")
	cmd := exec.Command(shellName(), shellArg(), updateCommand)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func shellName() string {
	if os.Getenv("ComSpec") != "" {
		return os.Getenv("ComSpec")
	}
	return "sh"
}

func shellArg() string {
	if os.Getenv("ComSpec") != "" {
		return "/C"
	}
	return "-c"
}

func truthy(value string) bool {
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}
