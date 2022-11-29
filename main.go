// Command statediff reports whether a GitHub PR touches a function or method that is potentially called by
// one or more root functions or methods. The PR is loaded through the GitHub API, roots are specified by
// the `-root` flag. Methods use the form
//
//	example.com/pkg/path.Type.Method
//
// functions use
//
//	example.com/pkg/path.Function
//
// If the PR touches one or more callstacks, they're posted in a comment on the PR (once).
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

var (
	dir        = flag.String("dir", ".", "base directory for the patch")
	ghtoken    = flag.String("ghtoken", "", "the GitHub API token")
	apiurl     = flag.String("apiurl", "https://api.github.com", "GitHub API URL")
	repository = flag.String("repository", "", "the GitHub owner/repository")
	prnum      = flag.Int("pr", 0, "the GitHub pull request number")
	rootNames  = stringSlice{}
)

const commentTitle = "PR potentially affects state code."

func init() {
	flag.Var(&rootNames, "roots", "comma-separated list of root functions")
}

func main() {
	flag.Parse()
	if *prnum <= 0 {
		fmt.Fprintf(os.Stderr, "statediff: invalid PR number: %d\n", *prnum)
		os.Exit(1)
	}
	*dir, _ = filepath.Abs(*dir)

	ctx := context.Background()
	var ts oauth2.TokenSource
	if *ghtoken != "" {
		ts = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: *ghtoken},
		)
	}
	tc := oauth2.NewClient(ctx, ts)
	gh := github.NewClient(tc)
	split := strings.SplitN(*repository, "/", 2)
	owner, repo := split[0], split[1]
	pr, patch, err := getPatch(ctx, gh, owner, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statediff: %v\n", err)
		os.Exit(2)
	}
	notified, err := hasComment(ctx, gh, owner, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statediff: %v\n", err)
		os.Exit(2)
	}
	if notified {
		fmt.Fprint(os.Stderr, "statediff: ignoring PR because it was already commented\n")
		os.Exit(0)
	}
	if !pr.GetMergeable() {
		fmt.Fprint(os.Stderr, "statediff: ignoring non-mergeable PR\n")
		os.Exit(0)
	}

	fset := new(token.FileSet)
	hunks, err := runCheck(fset, *dir, patch, rootNames)
	if err != nil {
		fmt.Fprintf(os.Stderr, "statediff: %v\n", err)
		os.Exit(2)
	}
	if len(hunks) == 0 {
		os.Exit(0)
	}
	comment := new(bytes.Buffer)
	fmt.Fprintf(comment, "%s\n\nCallstacks:\n", commentTitle)
	for _, hunk := range hunks {
		fmt.Fprintf(comment, "```\n")
		for i := len(hunk.stack) - 1; i >= 0; i-- {
			e := hunk.stack[i]
			pos := fset.Position(e.pos)
			fmt.Fprintf(comment, "%s (%s:%d)\n", e.fun.FullName(), hunk.relFile, pos.Line)
		}
		fmt.Fprintf(comment, "```\n")
	}
	commentStr := comment.String()
	_, _, err = gh.Issues.CreateComment(ctx, owner, repo, *prnum, &github.IssueComment{
		Body: &commentStr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "statediff: %v\n", err)
		os.Exit(2)
	}
}

func hasComment(ctx context.Context, gh *github.Client, owner, repo string) (bool, error) {
	page := 0
	for {
		opt := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{Page: page}}
		comments, resp, err := gh.Issues.ListComments(ctx, owner, repo, *prnum, opt)
		if err != nil {
			return false, err
		}
		for _, comment := range comments {
			if strings.Contains(comment.GetBody(), commentTitle) {
				return true, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return false, nil
}

func getPatch(ctx context.Context, gh *github.Client, owner, repo string) (*github.PullRequest, *bytes.Buffer, error) {
	dur := time.Second
	retries := 0
	for {
		pr, _, err := gh.PullRequests.Get(ctx, owner, repo, *prnum)
		if err != nil {
			return nil, nil, err
		}
		if pr.Mergeable == nil {
			if retries > 5 {
				return nil, nil, fmt.Errorf("gave up waiting for mergeable PR; tried %d times", retries)
			}
			retries++
			time.Sleep(dur)
			dur *= 2
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "GET", pr.GetDiffURL(), nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", "application/vnd.github.v3.diff")
		patch := new(bytes.Buffer)
		if _, err := gh.Do(ctx, req, patch); err != nil {
			return nil, nil, err
		}
		return pr, patch, nil
	}
}
