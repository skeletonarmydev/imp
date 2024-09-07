package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/andygrunwald/go-jira"
	"github.com/slack-go/slack"
	"github.com/spf13/viper"
	"io/ioutil"
	"log"
	"os"
)

type Issue struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	ProjectKey  string `json:"project_key"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type SlackGeneralChannel struct {
	ChannelId   string `json:"channelId"`
	ChannelName string `json:"channelName"`
}

type User struct {
	Email            string `json:"email"`
	SlackDisplayName string `json:"slackDisplayName"`
}

type TeamMember struct {
	User User `json:"user"`
}

type Team struct {
	TeamId      string       `json:"teamId"`
	TeamMembers []TeamMember `json:"teamMembers"`
}

type Service struct {
	ServiceId           string              `json:"serviceId"`
	RepositoryUrls      []string            `json:"repositoryUrls"`
	IssueTrackerUrl     string              `json:"issueTrackerUrl"`
	SlackGeneralChannel SlackGeneralChannel `json:"slackGeneralChannel"`
	Team                Team                `json:"team"`
}

type Node struct {
	Services []Service `json:"nodes"`
}

type Data struct {
	NodeList Node `json:"services"`
}

type DataSet struct {
	Data Data `json:"data"`
}

func main() {

	// ----- Config ----!>
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("yaml")   // REQUIRED if the config file does not have the extension in the name
	viper.AddConfigPath(".")      // look for config in the working directory
	err := viper.ReadInConfig()   // Find and read the config file
	if err != nil {               // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

	//List of repositories to create tickets for
	repoFile := flag.String("file", "", "list of repositories")

	flag.Parse()

	if *repoFile == "" {
		println("Error: No repositories specified")
		println("Usage: ./imp:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	//Create Slack api client
	api := slack.New(viper.GetString("slack.token"))

	//Create Jira client
	tp := jira.BasicAuthTransport{
		Username: viper.GetString("jira.user"),
		Password: viper.GetString("jira.token"),
	}

	jiraClient, err := jira.NewClient(tp.Client(), viper.GetString("jira.baseurl"))
	if err != nil {
		log.Printf(err.Error())
		panic(err)
	}

	//Get the full list of services from BigBrother
	services := fetchServices()

	//Create a simple dictionary based on the repository
	repoLookup := createMap(services)

	//Fetch the list of repositories from the file (first column only)
	repositoryList := readRepositoryFile(*repoFile)

	//Loop and find the services associated to the repositories
	for _, itm := range repositoryList {
		service := repoLookup[itm]

		//Create Jira Issue
		issue := Issue{
			Name:        fmt.Sprintf("Migration: %s", service.ServiceId),
			Type:        "Task",
			ProjectKey:  viper.GetString("jira.projectKey"),
			Description: fmt.Sprintf("Code Repository: %s ", itm),
		}
		jiraIssue := addIssue(jiraClient, issue)
		log.Printf("Created ticket: %s", jiraIssue.Key)

		//Notify on Slack (should use the actual service channel, not the default)
		sendSlackNotification(api, viper.GetString("slack.defaultChannel"),
			fmt.Sprintf("Migration request for: %s\nJira ticket: %sbrowse/%s",
				service.ServiceId,
				viper.GetString("jira.baseurl"),
				jiraIssue.Key),
		)
	}

}

func sendSlackNotification(api *slack.Client, channelId string, message string) {

	channelID, timestamp, err := api.PostMessage(
		channelId,
		slack.MsgOptionText(message, false),
	)

	if err != nil {
		fmt.Printf("%s\n", err)
		return
	}
	log.Printf("Message successfully sent to channel %s at %s\n", channelID, timestamp)
}

func readRepositoryFile(fileName string) []string {

	f, _ := os.Open(fileName)
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = ','

	records, err := r.ReadAll()
	if err != nil {
		panic(err)
	}

	repositories := []string{}

	for _, itm := range records {
		repositories = append(repositories, itm[0])
	}

	return repositories
}

func fetchServices() []Service {

	//Reading from a local file, this should be replaced with an actual API call to BigBrother
	servicesJson, err := os.Open("services.json")
	if err != nil {
		fmt.Println(err)
	}
	defer servicesJson.Close()

	byteValue, _ := ioutil.ReadAll(servicesJson)
	var dataSet DataSet

	err = json.Unmarshal(byteValue, &dataSet)
	if err != nil {
		fmt.Println(err)
	}

	return dataSet.Data.NodeList.Services
}

func createMap(services []Service) map[string]Service {
	lookup := make(map[string]Service)

	for _, itm := range services {
		for _, repo := range itm.RepositoryUrls {
			lookup[repo] = itm
		}
	}

	return lookup
}

func addIssue(jiraClient *jira.Client, issue Issue) Issue {

	jiraIssue := jira.Issue{
		Fields: &jira.IssueFields{
			Summary: issue.Name,
			Type: jira.IssueType{
				Name: issue.Type,
			},
			Project: jira.Project{
				Key: issue.ProjectKey,
			},
			Description: issue.Description,
		},
	}

	respIssue, _, err := jiraClient.Issue.Create(&jiraIssue)
	if err != nil {
		log.Printf(err.Error())
		panic(err)
	}

	issue.Key = respIssue.Key

	return issue
}
