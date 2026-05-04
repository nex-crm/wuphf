// cmd/stargazer-emails — find email addresses for GitHub repository stargazers.
//
// Strategy (waterfall):
//  1. Fetch the stargazer's public GitHub profile email.
//  2. If not found, search their recent public commits for an email address.
//
// Usage:
//
//	GITHUB_TOKEN=ghp_... stargazer-emails -repo owner/repo
//	GITHUB_TOKEN=ghp_... stargazer-emails -repo owner/repo -out emails.csv
//	GITHUB_TOKEN=ghp_... stargazer-emails -repo owner/repo -limit 500
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const githubAPI = "https://api.github.com"

type stargazer struct {
	Login string `json:"login"`
}

type userProfile struct {
	Login   string `json:"login"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	HTMLURL string `json:"html_url"`
}

type commit struct {
	Commit struct {
		Author struct {
			Email string `json:"email"`
		} `json:"author"`
	} `json:"commit"`
}

func main() {
	repo := flag.String("repo", "", "GitHub repository in owner/repo format (required)")
	out := flag.String("out", "", "output CSV file (default: stdout)")
	limit := flag.Int("limit", 0, "max stargazers to process (0 = all)")
	flag.Parse()

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "error: -repo is required")
		flag.Usage()
		os.Exit(2)
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "error: GITHUB_TOKEN environment variable not set")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 15 * time.Second}

	var w *csv.Writer
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot create output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = csv.NewWriter(f)
	} else {
		w = csv.NewWriter(os.Stdout)
	}
	defer w.Flush()

	_ = w.Write([]string{"login", "name", "email", "source", "profile_url"})

	stars, err := fetchStargazers(client, token, *repo, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching stargazers: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "processing %d stargazers…\n", len(stars))

	found := 0
	for i, s := range stars {
		profile, err := fetchProfile(client, token, s.Login)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: profile fetch for %s: %v\n", s.Login, err)
			continue
		}

		email := strings.TrimSpace(profile.Email)
		source := "profile"

		// Waterfall to commit email when profile email is absent.
		if email == "" {
			email, err = fetchCommitEmail(client, token, s.Login)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: commit email fetch for %s: %v\n", s.Login, err)
			}
			if email != "" {
				source = "commit"
			}
		}

		if email != "" {
			found++
			_ = w.Write([]string{profile.Login, profile.Name, email, source, profile.HTMLURL})
			w.Flush()
		}

		if (i+1)%50 == 0 {
			fmt.Fprintf(os.Stderr, "  %d/%d processed, %d emails found so far\n", i+1, len(stars), found)
		}
	}

	fmt.Fprintf(os.Stderr, "done: %d/%d stargazers have a discoverable email\n", found, len(stars))
}

// fetchStargazers returns up to limit stargazers (0 = all).
func fetchStargazers(client *http.Client, token, repo string, limit int) ([]stargazer, error) {
	var all []stargazer
	page := 1
	for {
		url := fmt.Sprintf("%s/repos/%s/stargazers?per_page=100&page=%d", githubAPI, repo, page)
		var batch []stargazer
		if err := apiGet(client, token, url, &batch); err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		page++
	}
	return all, nil
}

// fetchProfile returns the public GitHub profile for a login.
func fetchProfile(client *http.Client, token, login string) (*userProfile, error) {
	url := fmt.Sprintf("%s/users/%s", githubAPI, login)
	var p userProfile
	if err := apiGet(client, token, url, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// fetchCommitEmail looks at the user's most recent public commits and returns
// the first non-noreply author email found.
func fetchCommitEmail(client *http.Client, token, login string) (string, error) {
	url := fmt.Sprintf("%s/search/commits?q=author:%s&sort=author-date&order=desc&per_page=10", githubAPI, login)
	var result struct {
		Items []commit `json:"items"`
	}
	if err := apiGet(client, token, url, &result); err != nil {
		return "", err
	}
	for _, c := range result.Items {
		email := c.Commit.Author.Email
		if email != "" && !strings.Contains(email, "noreply") {
			return email, nil
		}
	}
	return "", nil
}

// apiGet makes an authenticated GitHub API GET request and JSON-decodes the response.
func apiGet(client *http.Client, token, url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		reset := resp.Header.Get("X-RateLimit-Reset")
		if reset != "" {
			if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
				wait := time.Until(time.Unix(ts, 0)) + 2*time.Second
				if wait > 0 && wait < 10*time.Minute {
					fmt.Fprintf(os.Stderr, "rate-limited; sleeping %s\n", wait.Round(time.Second))
					time.Sleep(wait)
					return apiGet(client, token, url, out)
				}
			}
		}
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
