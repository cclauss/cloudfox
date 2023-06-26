package gcp

import (
	"context"
	"log"
	"net/http"
	"os"
	"fmt"
	"bufio"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	goauth2 "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/storage/v1"
	"google.golang.org/api/cloudasset/v1p1beta1"
	"github.com/BishopFox/cloudfox/internal"
	"github.com/BishopFox/cloudfox/globals"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"
)

var (
	logger = internal.NewLogger()
)

type GCPClient struct {
	Name string
	HTTPClient *http.Client
	Logger internal.Logger
	TokenSource *oauth2.TokenSource
	TokenInfo *goauth2.Tokeninfo
	CloudresourcemanagerService *cloudresourcemanager.Service
	OrganizationsService *cloudresourcemanager.OrganizationsService
	OrganizationsClient *resourcemanager.OrganizationsClient
	FoldersClient *resourcemanager.FoldersClient
	ProjectsClient *resourcemanager.ProjectsClient
	StorageService *storage.Service
	FoldersService *cloudresourcemanager.FoldersService
	ProjectsService *cloudresourcemanager.ProjectsService
	CloudAssetService *cloudasset.Service
	ResourcesService *cloudasset.ResourcesService
	IamPoliciesService *cloudasset.IamPoliciesService
}

func (g *GCPClient) init(profile string) {
	g.Logger = internal.NewLogger()
	ctx := context.Background()
	var (
		profiles []GCloudProfile
		client_profile *GCloudProfile
		err error
	)
	profiles = listAllProfiles()
	for _, p := range profiles {
		if (p.Name == profile) {
			client_profile = &p
			g.Name = profile
			break
		}
	}

	// Initiate an http.Client. The following GET request will be
	// authorized and authenticated on the behalf of the SDK user.
	g.HTTPClient = client_profile.oauth_conf.Client(ctx, &(client_profile.initial_token))

	// Create all the clients
	g.OrganizationsClient, err = resourcemanager.NewOrganizationsRESTClient(ctx, option.WithHTTPClient(g.HTTPClient))
	if (err != nil){
		g.Logger.Fatal(fmt.Sprintf("Could not initiate GCP Organization Client: %v", err))
	}
	g.FoldersClient, err = resourcemanager.NewFoldersRESTClient(ctx, option.WithHTTPClient(g.HTTPClient))
	if (err != nil){
		g.Logger.Fatal(fmt.Sprintf("Could not initiate GCP Folders Client: %v", err))
	}
	g.ProjectsClient, err = resourcemanager.NewProjectsRESTClient(ctx, option.WithHTTPClient(g.HTTPClient))
	if (err != nil){
		g.Logger.Fatal(fmt.Sprintf("Could not initiate GCP Projects Client: %v", err))
	}

	ts, err := google.DefaultTokenSource(ctx)
	if err != nil {
		log.Fatal(err)
	}
	g.TokenSource = &ts
	oauth2Service, err := goauth2.NewService(ctx, option.WithHTTPClient(g.HTTPClient))
	tokenInfo, err := oauth2Service.Tokeninfo().Do()
	if err != nil {
		log.Fatal(err)
	}
	g.TokenInfo = tokenInfo
	cloudresourcemanagerService, err := cloudresourcemanager.NewService(ctx, option.WithHTTPClient(g.HTTPClient))
	if err != nil {
		log.Fatal(err)
	}
	g.CloudresourcemanagerService = cloudresourcemanagerService

	cloudassetService, err := cloudasset.NewService(ctx, option.WithHTTPClient(g.HTTPClient))
	if err != nil {
		log.Fatal(err)
	}
	g.CloudAssetService = cloudassetService

	storageService, err := storage.NewService(ctx, option.WithHTTPClient(g.HTTPClient))
	g.StorageService = storageService

	g.ResourcesService = cloudasset.NewResourcesService(cloudassetService)
	g.IamPoliciesService = cloudasset.NewIamPoliciesService(cloudassetService)
	g.OrganizationsService = cloudresourcemanager.NewOrganizationsService(cloudresourcemanagerService)
	g.FoldersService = cloudresourcemanager.NewFoldersService(cloudresourcemanagerService)
	g.ProjectsService = cloudresourcemanager.NewProjectsService(cloudresourcemanagerService)
	
}

func (g *GCPClient) GetResourcesRoots(organizations []string, folders []string, projects []string) []*internal.Node {
	var (
		roots []*internal.Node
		root internal.Node
		current *internal.Node
	)
	ctx := context.Background()
	root = internal.Node{ID: g.Name}
	organizationIterator := g.OrganizationsClient.SearchOrganizations(ctx, &resourcemanagerpb.SearchOrganizationsRequest{})
	for {
		organization, err := organizationIterator.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			g.Logger.FatalM(fmt.Sprintf("An error occurred when listing organizations: %v", err), globals.GCP_HIERARCHY_MODULE_NAME)
		}
		current = &internal.Node{ID: fmt.Sprintf("%s (%s)", organization.DisplayName, organization.Name)}
		//GCPLogger.Success(fmt.Sprintf("Listing stuff in organization %s", organization.DisplayName))
		// get childs (projects & folders) within current orgnization
		g.getChilds(current, organization.Name)
		root.Add(*current)
	}

	// now look for resources (projects) that do not have a parent and that live in the user' account directly
	// might need to investigate later but it looks like we can't have folders under a user account
	projectsListResponse, _ := g.CloudresourcemanagerService.Projects.List().Do()
	for _, project := range projectsListResponse.Projects {
		if project.Parent == nil {
			current = &internal.Node{ID: fmt.Sprintf("%s (%s)", project.Name, project.ProjectId)}
			root.Add(*current)
		}
	}
	roots = append(roots, &root)
	return roots
}

func (g *GCPClient) getChilds(current *internal.Node, parentName string){
	//GCPLogger.Success(fmt.Sprintf("Listing stuff in parent %s", parentName))
	var child internal.Node
	ctx := context.Background()
	folderIterator := g.FoldersClient.SearchFolders(ctx, &resourcemanagerpb.SearchFoldersRequest{Query: fmt.Sprintf("parent=%s", parentName)})
	for {
		folder, err := folderIterator.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			g.Logger.FatalM(fmt.Sprintf("An error occurred when listing folders in parent %s: %v", parentName, err), globals.GCP_HIERARCHY_MODULE_NAME)
		}
		child = internal.Node{ID: fmt.Sprintf("%s (%s)", folder.DisplayName, folder.Name)}
		g.getChilds(&child, folder.Name)
		(*current).Add(child)
	}
	projectIterator := g.ProjectsClient.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{Query: fmt.Sprintf("parent=%s", parentName)})
	for {
		project, err := projectIterator.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			g.Logger.FatalM(fmt.Sprintf("An error occurred when listing projects in parent %s: %v", parentName, err), globals.GCP_HIERARCHY_MODULE_NAME)
		}
		child = internal.Node{ID: fmt.Sprintf("%s (%s)", project.DisplayName, project.ProjectId)}
		(*current).Add(child)
	}
}

func NewGCPClient(profileName string) *GCPClient {
	client := new(GCPClient)
	client.init(profileName)
	return client
}

/*
	Get all usable GCP Profiles
	We are using only non expired user-tokens
*/
func GetAllGCPProfiles() []string {
	var (
		GCPProfiles []string
		accessTokens []Token
	)
	accessTokens = ReadAccessTokens()
	for _, accessToken := range accessTokens {
		if (!strings.Contains(accessToken.AccountID, "@")) {
			continue
		}

		exp, _ := time.Parse(time.RFC3339, accessToken.TokenExpiry)
		if exp.After(time.Now()) {
			GCPProfiles = append(GCPProfiles, accessToken.AccountID)
		}
	}
	return GCPProfiles
}

func ConfirmSelectedProfiles(GCPProfiles []string) bool {
	reader := bufio.NewReader(os.Stdin)
	logger.Info("Identified profiles:\n")
	for _, profile := range GCPProfiles {
		fmt.Printf("\t* %s\n", profile)
	}
	fmt.Printf("\n")
	logger.Info(fmt.Sprintf("Are you sure you'd like to run this command against the [%d] listed profile(s)? (Y\\n): ", len(GCPProfiles)))
	text, _ := reader.ReadString('\n')
	switch text {
	case "\n", "Y\n", "y\n":
		return true
	}
	return false

}

func GetSelectedGCPProfiles(GCPProfilesListPath string) []string {
	GCPProfilesListFile, err := internal.UtilsFs.Open(GCPProfilesListPath)
	internal.CheckErr(err, fmt.Sprintf("could not open given file %s", GCPProfilesListPath))
	if err != nil {
		fmt.Printf("\nError loading profiles. Could not open file at location[%s]\n", GCPProfilesListPath)
		os.Exit(1)
	}
	defer GCPProfilesListFile.Close()
	var GCPProfiles []string
	scanner := bufio.NewScanner(GCPProfilesListFile)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		profile := strings.TrimSpace(scanner.Text())
		if len(profile) != 0 {
			GCPProfiles = append(GCPProfiles, profile)
		}
	}
	return GCPProfiles
}
