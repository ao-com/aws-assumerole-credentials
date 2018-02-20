package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/user"

	"github.com/gczn/configparser"
	survey "gopkg.in/AlecAivazis/survey.v1"
)

var (
	domain          = "https://yourdomain.com"
	adfsURL         = fmt.Sprintf("%s/adfs/ls/IdpInitiatedSignOn.aspx?loginToRp=urn:amazon:webservices", domain)
	httpClient      = &http.Client{}
	username        string
	password        string
	initalQuestions = []*survey.Question{
		{
			Name:     "username",
			Prompt:   &survey.Input{Message: "Username:"},
			Validate: survey.Required,
		},
		{
			Name:     "password",
			Prompt:   &survey.Password{Message: "Password:"},
			Validate: survey.Required,
		},
	}
)

func main() {
	initialAnswers := struct {
		Username string
		Password string
	}{}
	err := survey.Ask(initalQuestions, &initialAnswers)
	roleSelection := &survey.Select{
		Message: "Choose a role:",
		Options: []string{},
	}
	samlResponse, err := GetSAMLResponse(initialAnswers.Username, initialAnswers.Password)
	if err != nil {
		panic(err)
	}

	roles := samlResponse.GetRoles()
	if len(roles) <= 0 {
		panic(errors.New("no roles for given user"))
	}

	for _, role := range roles {
		roleSelection.Options = append(roleSelection.Options, role.ARN)
	}

	roleSelection.Default = roles[0].ARN
	roleQuestion := []*survey.Question{
		{
			Name:   "role",
			Prompt: roleSelection,
		},
	}
	roleAnswer := struct {
		Role string
	}{}
	err = survey.Ask(roleQuestion, &roleAnswer)
	var selectedRole Role
	for _, role := range roles {
		if role.ARN == roleAnswer.Role {
			selectedRole = role
		}
	}

	creds, err := selectedRole.AssumeWithSAML(samlResponse.Base64)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Access key: %s\n", *creds.AccessKeyId)
	fmt.Printf("Secret key: %s\n", *creds.SecretAccessKey)
	fmt.Printf("Session token: %s\n", *creds.SessionToken)

	writeToFile := false
	prompt := &survey.Confirm{
		Message: "Do you want to write these credentials to the credentials file?",
	}
	survey.AskOne(prompt, &writeToFile, nil)
	if writeToFile == false {
		return
	}

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	configparser.Delimiter = " = "
	credentialsFile := fmt.Sprintf("%s/.aws/credentials", usr.HomeDir)
	config, err := configparser.Read(credentialsFile)
	if err != nil {
		panic(err)
	}

	configSectionName := "default"
	sectionPrompt := &survey.Input{
		Message: "Config section name:",
	}
	survey.AskOne(sectionPrompt, &configSectionName, nil)
	section, err := config.Section(configSectionName)
	if err != nil {
		section = config.NewSection(configSectionName)
	}

	section.Add("aws_access_key_id", *creds.AccessKeyId)
	section.Add("aws_secret_access_key", *creds.SecretAccessKey)
	section.Add("aws_session_token", *creds.SessionToken)

	err = configparser.Save(config, credentialsFile)
	if err != nil {
		panic(err)
	}
}
