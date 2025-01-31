package sdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/codebuild"
)

type MockedCodeBuildClient struct {
}

func (m *MockedCodeBuildClient) ListProjects(ctx context.Context, input *codebuild.ListProjectsInput, options ...func(*codebuild.Options)) (*codebuild.ListProjectsOutput, error) {
	return &codebuild.ListProjectsOutput{
		Projects: []string{
			"project1",
			"project2",
		},
	}, nil
}
