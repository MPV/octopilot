package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dailymotion-oss/octopilot/internal/git"
	"github.com/dailymotion-oss/octopilot/repository"
	"github.com/dailymotion-oss/octopilot/update"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

// the following build-related variables are set at release-time by goreleaser
// using ldflags
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

var options struct {
	updates []string
	repos   []string
	repository.UpdateOptions
	logLevel    string
	failOnError bool
}

func init() {
	// required flags
	pflag.StringArrayVarP(&options.updates, "update", "u", nil, `An update operation, such as "yaml(file=config.yaml,path='version')=file(path=VERSION)" - see the online documentation for all available updaters.`)
	assert(pflag.CommandLine.SetAnnotation("update", "mandatory", []string{"true"}))
	pflag.StringArrayVarP(&options.repos, "repo", "r", nil, `A repository to update, defined either statically in the form "org/repo", or dynamically with the "discover-from" prefix - see the online documentation for more details.`)
	assert(pflag.CommandLine.SetAnnotation("repo", "mandatory", []string{"true"}))
	pflag.StringVar(&options.GitHub.AuthMethod, "github-auth-method", "token", `Mandatory GitHub authentication method: either "token" or "app" - see the online documentation for more details.`)
	assert(pflag.CommandLine.SetAnnotation("github-auth-method", "mandatory", []string{"true"}))

	// GitHub auth flags
	pflag.StringVar(&options.GitHub.Token, "github-token", os.Getenv("GITHUB_TOKEN"), `This is the GitHub token - required when the GitHub auth method is "token". Default to the GITHUB_TOKEN env var.`)
	pflag.Int64Var(&options.GitHub.AppID, "github-app-id", int64(getenvInt("GITHUB_APP_ID")), `This is the GitHub AppID - required when the GitHub auth method is "app". Default to the GITHUB_APP_ID env var.`)
	pflag.Int64Var(&options.GitHub.InstallationID, "github-installation-id", int64(getenvInt("GITHUB_INSTALLATION_ID")), "For the `app` GitHub auth method, contains the GitHubApp Installation ID. Default to the GITHUB_INSTALLATION_ID env var.")
	pflag.StringVar(&options.GitHub.PrivateKey, "github-privatekey", os.Getenv("GITHUB_PRIVATEKEY"), "For the `app` GitHub auth method, contains the GitHubApp Private key file in PEM format. Default to the GITHUB_PRIVATEKEY env var.")
	pflag.StringVar(&options.GitHub.PrivateKeyPath, "github-privatekey-path", os.Getenv("GITHUB_PRIVATEKEY_PATH"), "For the `app` GitHub auth method, contains the GitHubApp Private key file path `/some/key.pem` (used if the github-privatekey is empty). Default to the GITHUB_PRIVATEKEY_PATH env var.")
	pflag.StringVar(&options.GitHub.URL, "github-url", repository.PublicGithubURL, `GitHub server URL`)

	// pull-request flags
	pflag.StringVar(&options.GitHub.PullRequest.Title, "pr-title", "", "The title of the Pull Request to create. Default to the commit title.")
	pflag.StringVar(&options.GitHub.PullRequest.TitleUpdateOperation, "pr-title-update-operation", "", `The type of operation when updating the PR's title: "ignore" (keep old value), "replace", "prepend" or "append". Default is: "ignore" for "append" strategy, "replace" for "reset" strategy, and not applicable for "recreate" strategy.`)
	pflag.StringVar(&options.GitHub.PullRequest.Body, "pr-body", "", "The body of the Pull Request to create. Default to the commit body and the commit footer.")
	pflag.StringVar(&options.GitHub.PullRequest.BodyUpdateOperation, "pr-body-update-operation", "", `The type of operation when updating the PR's body: "ignore" (keep old value), "replace", "prepend" or "append". Default is: "ignore" for "append" strategy, "replace" for "reset" strategy, and not applicable for "recreate" strategy.`)
	pflag.StringArrayVar(&options.GitHub.PullRequest.Comments, "pr-comment", []string{}, "List of comments to add to the Pull Request.")
	pflag.StringSliceVar(&options.GitHub.PullRequest.Labels, "pr-labels", []string{"octopilot-update"}, "List of labels set on the pull requests, and used to find existing pull requests to update.")
	pflag.StringVar(&options.GitHub.PullRequest.BaseBranch, "pr-base-branch", "master", "Name of the branch used as a base when creating pull requests.")
	pflag.BoolVar(&options.GitHub.PullRequest.Draft, "pr-draft", false, `Create "draft" Pull Requests, instead of regular ones. It means that the PRs can't be merged until marked as "ready for review".`)
	pflag.BoolVar(&options.GitHub.PullRequest.Merge.Enabled, "pr-merge", false, `Automatically merge the Pull Requests created. It will wait until the PRs are "mergeable" before merging them.`)
	pflag.StringVar(&options.GitHub.PullRequest.Merge.Method, "pr-merge-method", "merge", `If auto-merge is enabled, the PRs will be merged with this method. Can be either "merge", "squash", or "rebase".`)
	pflag.StringVar(&options.GitHub.PullRequest.Merge.CommitTitle, "pr-merge-commit-title", "", "If auto-merge is enabled, this is the optional title of the merge commit.")
	pflag.StringVar(&options.GitHub.PullRequest.Merge.CommitMessage, "pr-merge-commit-message", "", "If auto-merge is enabled, this is the optional body of the merge commit.")
	pflag.StringVar(&options.GitHub.PullRequest.Merge.SHA, "pr-merge-sha", "", "If auto-merge is enabled, this is the optional SHA that pull request head must match to allow merge.")
	pflag.DurationVar(&options.GitHub.PullRequest.Merge.PollTimeout, "pr-merge-poll-timeout", 10*time.Minute, "If auto-merge is enabled, this is the maximum duration to wait for a Pull Request to be mergeable.")
	pflag.DurationVar(&options.GitHub.PullRequest.Merge.PollInterval, "pr-merge-poll-interval", 30*time.Second, "If auto-merge is enabled, this is the duration to wait for between each GitHub API call to check if a PR is mergeable.")
	pflag.IntVar(&options.GitHub.PullRequest.Merge.RetryCount, "pr-merge-retry-count", 3, "If auto-merge is enabled, this is the number of times to retry the merge operation in case of merge failure.")

	// git-related flags
	pflag.StringVar(&options.UpdateOptions.Git.CloneDir, "git-clone-dir", temporaryDirectory(), "Directory used to clone the repositories.")
	pflag.StringArrayVar(&options.UpdateOptions.Git.StagePatterns, "git-stage-pattern", nil, "List of path patterns that will be added to the git index and committed.")
	pflag.BoolVar(&options.UpdateOptions.Git.StageAllChanged, "git-stage-all-changed", true, "Commit all files changed.")
	pflag.StringVar(&options.UpdateOptions.Git.AuthorName, "git-author-name", firstNonEmpyValue(os.Getenv("GIT_AUTHOR_NAME"), git.ConfigValue("user.name")), `Name of the author of the git commit. Default to the GIT_AUTHOR_NAME env var, or the "user.name" git config value.`)
	pflag.StringVar(&options.UpdateOptions.Git.AuthorEmail, "git-author-email", firstNonEmpyValue(os.Getenv("GIT_AUTHOR_EMAIL"), git.ConfigValue("user.email")), `Email of the author of the git commit. Default to the GIT_AUTHOR_EMAIL env var, or the "user.email" git config value.`)
	pflag.StringVar(&options.UpdateOptions.Git.CommitterName, "git-committer-name", firstNonEmpyValue(os.Getenv("GIT_COMMITTER_NAME"), git.ConfigValue("user.name")), `Name of the committer. Default to the GIT_COMMITTER_NAME env var, or the "user.name" git config value.`)
	pflag.StringVar(&options.UpdateOptions.Git.CommitterEmail, "git-committer-email", firstNonEmpyValue(os.Getenv("GIT_COMMITTER_EMAIL"), git.ConfigValue("user.email")), `Email of the committer. Default to the GIT_COMMITTER_EMAIL env var, or the "user.email" git config value.`)
	pflag.StringVar(&options.UpdateOptions.Git.CommitTitle, "git-commit-title", "", "Title of the git commit.")
	pflag.StringVar(&options.UpdateOptions.Git.CommitBody, "git-commit-body", "", "Body of the git commit.")
	pflag.StringVar(&options.UpdateOptions.Git.CommitFooter, "git-commit-footer", defaultCommitFooter(), "Footer of the git commit.")
	pflag.StringVar(&options.UpdateOptions.Git.BranchPrefix, "git-branch-prefix", "octopilot-", "Prefix of the new branch to create.")
	pflag.StringVar(&options.UpdateOptions.Git.SigningKeyPath, "git-signing-key-path", os.Getenv("GIT_SIGNING_KEY_PATH"), "Path to the private key file to sign commits or tags (e.g. `/some/key.pgp`). Default to the GIT_SIGNING_KEY_PATH env var.")
	pflag.StringVar(&options.UpdateOptions.Git.SigningKeyPassphrase, "git-signing-key-passphrase", os.Getenv("GIT_SIGNING_KEY_PASSPHRASE"), "Passphrase to decrypt the signing key. Default to the GIT_SIGNING_KEY_PASSPHRASE env var.")

	pflag.StringVar(&options.Strategy, "strategy", "reset", `Strategy to use when creating/updating the Pull Requests: either "reset" (reset any existing PR from the current base branch), "append" (append new commit to any existing PR) or "recreate" (always create a new PR).`)
	pflag.BoolVar(&options.KeepFiles, "keep-files", false, "Keep the cloned repositories on disk. If false, the files will be deleted at the end of the process.")
	pflag.BoolVarP(&options.DryRun, "dry-run", "n", false, `Don't perform any operation on the remote git repository: all operations will be done in the local cloned repository. You should also set the "--keep-files" flag to keep the files and inspect the changes in the local repository.`)
	pflag.StringVar(&options.logLevel, "log-level", "info", "Log level. Supported values: trace, debug, info, warning, error, fatal, panic.")

	pflag.BoolVar(&options.failOnError, "fail-on-error", false, "Exit with error code 1 if any repository update fails.")
	pflag.BoolP("help", "h", false, "Display this help message.")
	pflag.Bool("version", false, "Display the version and exit.")

	// usage
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Octopilot v%s - Documentation at https://dailymotion-oss.github.io/octopilot/v%s/\n", buildVersion, buildVersion)
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		pflag.PrintDefaults()
	}
}

func main() {
	ctx := context.Background()
	pflag.Parse()
	printHelpOrVersion()
	setLogLevel()
	checkMandatoryFlags()

	logrus.WithField("updates", options.updates).Trace("Parsing updates")
	updaters, err := update.Parse(options.updates)
	if err != nil {
		logrus.
			WithError(err).
			WithField("updates", options.updates).
			Fatal("Failed to parse updates")
	}
	logrus.WithField("updaters", updaters).Debug("Updaters ready")

	logrus.WithField("repos", options.repos).Trace("Parsing repositories")
	repositories, err := repository.Parse(ctx, options.repos, options.GitHub)
	if err != nil {
		logrus.
			WithError(err).
			WithField("repos", options.repos).
			Fatal("Failed to parse repos")
	}
	logrus.WithField("repositories", repositories).Debug("Repositories ready")

	logrus.WithField("repositories-count", len(repositories)).Trace("Starting updates")
	var wg sync.WaitGroup
	errors := make(chan error, len(repositories))
	for _, repo := range repositories {
		wg.Add(1)
		go func(repo repository.Repository) {
			defer wg.Done()
			logrus.WithField("repository", repo.FullName()).Trace("Starting repository update")
			updated, err := repo.Update(ctx, updaters, options.UpdateOptions)
			if err != nil {
				logrus.
					WithError(err).
					WithField("repository", repo.FullName()).
					Error("Repository update failed")
				errors <- err
				return
			}
			if !updated {
				logrus.WithField("repository", repo.FullName()).Warn("Repository update has no changes")
				return
			}
			logrus.WithField("repository", repo.FullName()).Info("Repository update finished")
		}(repo)
	}
	wg.Wait()
	close(errors)
	logrus.WithField("repositories-count", len(repositories)).Info("Updates finished")

	if options.failOnError && len(errors) > 0 {
		logrus.Fatal("Some repository updates failed")
	}
}

func checkMandatoryFlags() {
	var missingFlags []string
	pflag.CommandLine.VisitAll(func(flag *pflag.Flag) {
		if mandatory, found := flag.Annotations["mandatory"]; found {
			for _, v := range mandatory {
				if isMandatory, _ := strconv.ParseBool(v); isMandatory {
					switch flag.Value.Type() {
					case "string":
						if len(flag.Value.String()) == 0 {
							missingFlags = append(missingFlags, flag.Name)
						}
					case "stringSlice":
						if flag.Value.String() == "[]" {
							missingFlags = append(missingFlags, flag.Name)
						}
					}
				}
			}
		}
	})

	if len(missingFlags) == 0 {
		return
	}

	logrus.WithField("missing-flags", missingFlags).Fatal("Mandatory fields not defined")
}

func setLogLevel() {
	level, err := logrus.ParseLevel(options.logLevel)
	if err != nil {
		logrus.
			WithError(err).
			WithField("log-level", options.logLevel).
			Fatal("Invalid log level")
	}
	logrus.SetLevel(level)
}

func printHelpOrVersion() {
	if printHelp, _ := pflag.CommandLine.GetBool("help"); printHelp {
		fmt.Printf("Octopilot version %v, commit %v, built at %v\n", buildVersion, buildCommit, buildDate)
		pflag.Usage()
		os.Exit(0)
	}

	if printVersion, _ := pflag.CommandLine.GetBool("version"); printVersion {
		fmt.Printf("version %v, commit %v, built at %v", buildVersion, buildCommit, buildDate)
		os.Exit(0)
	}
}

func temporaryDirectory() string {
	dir, err := os.MkdirTemp("", "octopilot")
	if err != nil {
		dir = filepath.Join(os.TempDir(), "octopilot")
	}
	return dir
}

func defaultCommitFooter() string {
	footer := new(strings.Builder)
	footer.WriteString("Generated by [Octopilot](https://github.com/dailymotion-oss/octopilot)")
	if buildVersion == "dev" {
		footer.WriteString(" (dev version)")
	} else {
		footer.WriteString(fmt.Sprintf(" [v%[1]s](https://github.com/dailymotion-oss/octopilot/releases/tag/v%[1]s)", buildVersion))
	}
	if repoURL := git.CurrentRepositoryURL(); len(repoURL) > 0 {
		footer.WriteString(fmt.Sprintf(" from %s", repoURL))
	} else if currentDir, err := os.Getwd(); err == nil {
		dirName := filepath.Base(currentDir)
		footer.WriteString(fmt.Sprintf(" from %s", dirName))
	}
	return footer.String()
}

func firstNonEmpyValue(values ...string) string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return ""
}

func assert(err error) {
	if err != nil {
		panic(err)
	}
}

func getenvInt(key string) int {
	s := os.Getenv(key)
	if s != "" {
		v, err := strconv.Atoi(s)
		assert(err)
		return v
	}
	return 0
}
