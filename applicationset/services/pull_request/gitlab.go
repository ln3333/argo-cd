package pull_request

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/hashicorp/go-retryablehttp"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/argoproj/argo-cd/v3/applicationset/utils"
)

type GitLabService struct {
	client           *gitlab.Client
	project          string
	labels           []string
	pullRequestState string
}

var _ PullRequestService = (*GitLabService)(nil)

func NewGitLabService(token, url, project string, labels []string, pullRequestState string, scmRootCAPath string, insecure bool, caCerts []byte) (PullRequestService, error) {
	var clientOptionFns []gitlab.ClientOptionFunc

	// Set a custom Gitlab base URL if one is provided
	if url != "" {
		clientOptionFns = append(clientOptionFns, gitlab.WithBaseURL(url))
	}

	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = utils.GetTlsConfig(scmRootCAPath, insecure, caCerts)

	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Transport = tr

	clientOptionFns = append(clientOptionFns, gitlab.WithHTTPClient(retryClient.HTTPClient))

	client, err := gitlab.NewClient(token, clientOptionFns...)
	if err != nil {
		return nil, fmt.Errorf("error creating Gitlab client: %w", err)
	}

	return &GitLabService{
		client:           client,
		project:          project,
		labels:           labels,
		pullRequestState: pullRequestState,
	}, nil
}

func (g *GitLabService) List(ctx context.Context) ([]*PullRequest, error) {
	// Filter the merge requests on labels, if they are specified.
	var labels *gitlab.LabelOptions
	if len(g.labels) > 0 {
		var labelsList gitlab.LabelOptions = g.labels
		labels = &labelsList
	}
	opts := &gitlab.ListProjectMergeRequestsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
		Labels: labels,
	}

	if g.pullRequestState != "" {
		opts.State = &g.pullRequestState
	}

	pullRequests := []*PullRequest{}
	for {
		mrs, resp, err := g.client.MergeRequests.ListProjectMergeRequests(g.project, opts, gitlab.WithContext(ctx))
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				// return a custom error indicating that the repository is not found,
				// but also returning the empty result since the decision to continue or not in this case is made by the caller
				return pullRequests, NewRepositoryNotFoundError(err)
			}
			return nil, fmt.Errorf("error listing merge requests for project '%s': %w", g.project, err)
		}
		for _, mr := range mrs {
			pullRequests = append(pullRequests, &PullRequest{
				Number:       mr.IID,
				Title:        mr.Title,
				Branch:       mr.SourceBranch,
				TargetBranch: mr.TargetBranch,
				HeadSHA:      mr.SHA,
				Labels:       mr.Labels,
				Author:       mr.Author.Username,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return pullRequests, nil
}
