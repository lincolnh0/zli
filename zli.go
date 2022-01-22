package main

import (
	"encoding/json"
	"fmt"
	"github.com/AlecAivazis/survey/v2"
	"github.com/manifoldco/promptui"
	"github.com/spf13/viper"
	"github.com/urfave/cli"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"zli/config"
)

var app = cli.NewApp()

var homedir, _ = os.UserHomeDir()
var baseConfigFile = homedir + "/.config/zli.yml"
var configuration config.JenkinsConfigurations

// Function to set initial information about the tool.
func info() {
	app.Name = "ZLI"
	app.Usage = "A CLI for Jenkins heavy workflow"
	app.Author = "lincolnh0"
	app.Version = "0.1.0"
}

// Load config file content to config object.
func loadConfig() config.JenkinsConfigurations {

	viper.SetConfigName("zli")
	viper.SetConfigType("yml")
	viper.SetConfigFile(baseConfigFile)

	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("Error reading config file, %s\n", err)
		viper.Set("JenkinsUrl", "")
		viper.Set("JenkinsUser", "")
		viper.Set("JenkinsAPI", "")
	}

	err := viper.Unmarshal(&configuration)
	if err != nil {
		fmt.Printf("Unable to decode into struct, %v", err)
	}

	save := false
	var jenkinsUrl string
	var user string
	var api string
	if configuration.JenkinsURL == "" {
		fmt.Println("Please enter your Jenkins URL")
		fmt.Scanln(&jenkinsUrl)
		if !strings.HasSuffix(jenkinsUrl, "/") {
			jenkinsUrl += "/"
		}
		viper.Set("JenkinsUrl", jenkinsUrl)
		save = true
	}

	if configuration.JenkinsUser == "" {
		fmt.Println("Please enter your Jenkins username")
		fmt.Scanln(&user)
		viper.Set("JenkinsUser", user)
		save = true
	}

	if configuration.JenkinsAPI == "" {
		fmt.Println("Please enter your Jenkins API token")
		fmt.Printf("Generate here: %suser/%s/configure\n", jenkinsUrl, user)
		fmt.Scanln(&api)
		viper.Set("JenkinsAPI", api)
		save = true
	}

	if save {
		saveConfig()
	}

	return configuration
}

func saveConfig() bool {

	err := viper.WriteConfigAs(baseConfigFile)
	if err != nil {
		log.Fatalf("Error creating file, %s\n", err)
	}
	return true
}

func main() {
	loadConfig()
	info()
	commands()

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func commands() {
	app.Commands = []cli.Command{
		{
			Name:      "deploy",
			Aliases:   []string{"d"},
			Usage:     "Deploy a site with its Jenkins job.",
			ArgsUsage: "[site alias]",
			Action:    deploy,
		},
		{
			Name:    "list",
			Aliases: []string{"l"},
			Usage:   "List all the aliases and job mapping stored locally",
			Action:  list,
		},
		{
			Name:      "add",
			Aliases:   []string{"a"},
			Usage:     "Add a new site alias and its Jenkins URL mapping",
			ArgsUsage: "[site alias] [job's sub url]",
			Action:    add,
		},
		{
			Name:      "remove",
			Aliases:   []string{"rm"},
			Usage:     "Remove a site alias mapping",
			ArgsUsage: "[site alias]",
			Action:    remove,
		},
	}
}

// Deploy job by alias.
func deploy(c *cli.Context) {
	if len(c.Args()) < 1 {
		log.Fatalln("Please enter a site alias.")
	}
	siteAlias := c.Args()[0]
	var site config.JenkinsJob
	for _, job := range configuration.Jobs {
		if siteAlias == job.Alias {
			site = job
			break
		}
	}

	if site.Alias == "" || site.URL == "" {
		log.Fatalf("Site alias %s cannot be found\n", siteAlias)
	}
	parameters := getJobParameters(loadConfig(), site)
	var drushCommands []string
	if len(parameters) > 0 {
		drushCommands = checkboxes("Select deploy parameters", parameters)
	}
	fmt.Println("This will deploy", site.Alias, "with parameters", drushCommands)
	if yesNo() {
		success, status := deployWithParameters(configuration, site, drushCommands)
		if success {
			fmt.Printf("%s deployed successfully", site.Alias)
		} else {
			fmt.Println(status)
		}
	} else {
		fmt.Println("Deploy abandoned.")
	}
}

// List all aliases.
func list(c *cli.Context) {
	configuration = loadConfig()
	for _, job := range configuration.Jobs {
		fmt.Println("-", job.Alias, configuration.JenkinsURL+job.URL)
	}
}

// Add new alias.
func add(c *cli.Context) {
	if len(c.Args()) != 2 {
		log.Fatalln("Please enter a site alias and the Jenkins job URL")
	}

	formattedUrl := c.Args()[1]
	if strings.HasPrefix(c.Args()[1], "/") {
		formattedUrl = formattedUrl[1:]
	}
	if !strings.HasSuffix(c.Args()[1], "/") {
		formattedUrl += "/"
	}
	jobUrl := configuration.JenkinsURL + formattedUrl + "api/json"

	// Perform a basic get to validate URL.
	response := getFromJenkins(configuration, jobUrl)
	var result map[string]interface{}

	err := json.Unmarshal(response, &result)
	if err != nil {
		log.Fatalln("Error when decoding Jenkins job information", err)
	}
	if projectName := result["fullDisplayName"]; projectName != nil {
		fmt.Printf("Do you want to add '%s' as %s\n", projectName, c.Args()[0])
		if yesNo() {
			newJobsList := append(configuration.Jobs, config.JenkinsJob{
				Alias: c.Args()[0],
				URL:   formattedUrl,
			})
			sort.Slice(newJobsList, func(i, j int) bool {
				return newJobsList[i].Alias < newJobsList[j].Alias
			})
			viper.Set("Jobs", newJobsList)
			saveConfig()
			fmt.Println(projectName, "successfully added as", c.Args()[0])
		}
		return
	}
	log.Fatalln("Please double check your URL pattern. Only copy from the first /job onwards.")

}

// Remove existing alias.
func remove(c *cli.Context) {

	if len(c.Args()) != 1 {
		log.Fatalln("Please enter an alias as argument")
	}

	siteAlias := c.Args()[0]
	removed := false
	for index, job := range configuration.Jobs {
		if siteAlias == job.Alias {
			newJobsList := append(configuration.Jobs[:index], configuration.Jobs[index+1:]...)
			sort.Slice(newJobsList, func(i, j int) bool {
				return newJobsList[i].Alias < newJobsList[j].Alias
			})
			viper.Set("Jobs", newJobsList)
			saveConfig()
			removed = true
			break
		}
	}

	if removed {
		fmt.Printf("%s has been removed\n", siteAlias)
	} else {
		log.Fatalf("%s cannt be found\n", siteAlias)
	}

}

// Retrieve jenkins job parameter.
func getJobParameters(configuration config.JenkinsConfigurations, job config.JenkinsJob) []string {
	jobUrl := configuration.JenkinsURL + job.URL + "api/json?tree=property[parameterDefinitions[name,description,type]]"
	response := getFromJenkins(configuration, jobUrl)
	var options []string
	var result map[string]interface{}

	err := json.Unmarshal(response, &result)
	if err != nil {
		log.Fatalln("Error when decoding Jenkins job configurations", err)
	}
	for _, val := range result["property"].([]interface{}) {
		if parameters := val.(map[string]interface{})["parameterDefinitions"]; parameters != nil {
			for _, parameter := range parameters.([]interface{}) {
				if parameter.(map[string]interface{})["type"] == "BooleanParameterDefinition" {
					parameterName := (parameter.(map[string]interface{})["name"]).(string)
					options = append(options, parameterName)
				}
			}
		}
	}

	return options

}

// Generic request handler for Jenkins GET requests.
func getFromJenkins(configuration config.JenkinsConfigurations, endpoint string) []byte {
	req, err := http.NewRequest("GET", endpoint, nil)
	req.SetBasicAuth(configuration.JenkinsUser, configuration.JenkinsAPI)
	req.Header.Set("Accept", "application/json")
	if err != nil {
		log.Fatalln("Failed to reach Jenkins.")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Fatalln(err)
	}

	return b
}

// Post request to job build page.
func deployWithParameters(configuration config.JenkinsConfigurations, job config.JenkinsJob, parameters []string) (bool, string) {
	jobUrl := configuration.JenkinsURL + job.URL + "buildWithParameters"
	data := url.Values{}
	for _, item := range parameters {
		data.Add(item, "true")
	}

	req, _ := http.NewRequest("POST", jobUrl, strings.NewReader(data.Encode()))
	req.SetBasicAuth(configuration.JenkinsUser, configuration.JenkinsAPI)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	_, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Fatalln(err)
	}

	return resp.Status == "201", resp.Status
}

// User-Interface helper functions.
// Provide a selection prompt.
func yesNo() bool {
	prompt := promptui.Select{
		Label: "Confirm[Yes/No]",
		Items: []string{"Yes", "No"},
	}
	_, result, err := prompt.Run()
	if err != nil {
		log.Fatalf("Prompt failed %v\n", err)
	}
	return result == "Yes"
}

// Return strings of selected checkboxes.
func checkboxes(label string, opts []string) []string {
	var res []string
	prompt := &survey.MultiSelect{
		Message: label,
		Options: opts,
	}
	err := survey.AskOne(prompt, &res)
	if err != nil {
		return nil
	}

	return res
}
