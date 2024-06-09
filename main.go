package main

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"code.gitea.io/sdk/gitea"
	"github.com/google/go-github/github"
	"github.com/sethvargo/go-envconfig"
	"golang.org/x/oauth2"
)

const (
	DefaultGiteaUrl = "https://git.ishanjain.me"
)

// Sync every 1 hour 01 minutes
const DefaultMirrorInterval = "1h01m0s"

type Config struct {
	GithubUser  string `env:"GITHUB_USER,required"`
	GithubToken string `env:"GITHUB_TOKEN,required"`
	GiteaToken  string `env:"GITEA_TOKEN,required"`
	GiteaUser   string `env:"GITEA_USER,required"`
	GiteaUrl    string `env:"GITEA_URL"`
}

func readConfig() Config {
	ctx := context.Background()
	config := Config{}

	err := envconfig.Process(ctx, &config)
	if err != nil {
		log.Panicf("error in reading env var: %v", err.Error())
	}

	if config.GiteaUrl == "" {
		config.GiteaUrl = DefaultGiteaUrl
	}
	return config
}

func main() {
	config := readConfig()

	// Github API
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: config.GithubToken})
	oclient := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(oclient)

	giteaClient, err := gitea.NewClient(config.GiteaUrl, gitea.SetBasicAuth(config.GiteaUser, config.GiteaToken))
	if err != nil {
		log.Errorf("error in creating client: %v", err.Error())
	}

	log.Infoln("fetching a list of repositories on Gitea")
	giteaRepos := listGiteaRepositories(config, giteaClient)

	log.WithFields(log.Fields{
		"gitea_repo_count": len(giteaRepos),
	}).Infoln("fetched a list of repositories on gitea")

	// For each repo, Create a a repository on Github with the same visiblity settings
	// Setup synchronisation operation with the github repo

	for _, repo := range giteaRepos {
		log.WithFields(log.Fields{
			"name":    repo.Name,
			"org":     repo.Owner.UserName,
			"private": repo.Private,
		}).Infoln("configuring repo")

		repoCreated := setupRepo(config, repo, ghClient)

		if repoCreated {
			setupMirror(config, repo, giteaClient)
		}
	}
}

func setupMirror(config Config, repo *gitea.Repository, client *gitea.Client) {

	client.PushMirrors(repo.Owner.UserName, repo.Name, gitea.CreatePushMirrorOption{
		Interval:       DefaultMirrorInterval,
		RemoteAddress:  fmt.Sprintf("https://github.com/%s/%s", config.GithubUser, repo.Name),
		RemotePassword: config.GithubToken,
		RemoteUsername: config.GithubUser,
		SyncONCommit:   true,
	})

}

func setupRepo(config Config, repo *gitea.Repository, client *github.Client) bool {
	if repo.OriginalURL != "" {
		return false
	}

	if githubExists(client, config.GithubUser, repo.Name) {
		// Create repository if it doesn't exist already
		_, _, err := client.Repositories.Create(context.Background(), "", &github.Repository{
			Name:        &repo.Name,
			Private:     &repo.Private,
			Description: &repo.Description,
			HasWiki:     &repo.HasWiki,
			HasProjects: &repo.HasProjects,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"name":    repo.Name,
				"org":     repo.Owner.UserName,
				"private": repo.Private,
				"error":   err.Error(),
			}).Errorln("error in creating repository")
		}

		return true
	}

	// Copy description, private state and other parameters if it exists
	_, _, err := client.Repositories.Edit(context.Background(), config.GithubUser, repo.Name, &github.Repository{
		Private:     &repo.Private,
		Description: &repo.Description,
		HasWiki:     &repo.HasWiki,
		HasProjects: &repo.HasProjects,
	})
	if err != nil {
		log.WithFields(log.Fields{
			"name":    repo.Name,
			"org":     repo.Owner.UserName,
			"private": repo.Private,
			"error":   err.Error(),
		}).Errorln("error in updating repository")
	}

	return false
}

func githubExists(client *github.Client, owner, repo string) bool {

	_, _, err := client.Repositories.Get(context.Background(), owner, repo)
	if err != nil {
		return false
	}

	return true
}

func listGiteaRepositories(config Config, client *gitea.Client) []*gitea.Repository {
	page := 1
	output := []*gitea.Repository{}

	for {

		repos, _, err := client.ListUserRepos(config.GiteaUser, gitea.ListReposOptions{
			ListOptions: gitea.ListOptions{
				Page:     page,
				PageSize: 20,
			},
		})
		if err != nil {
			log.Errorf("error in listing repositories: %v", err.Error())
			break
		}

		if len(repos) == 0 {
			break
		}

		output = append(output, repos...)
		page += 1
	}

	return output
}
