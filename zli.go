package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"zli/config"

	"github.com/AlecAivazis/survey/v2"
	"github.com/manifoldco/promptui"
	"github.com/spf13/viper"
	"github.com/urfave/cli"
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
	app.Version = "0.3.0"
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
			Usage:     "Deploy a site with its Jenkins job",
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
		{
			Name:      "status",
			Aliases:   []string{"s"},
			Usage:     "Display the status of a build for a job, defaults to latest",
			ArgsUsage: "[site alias] [build number]",
			Action:    status,
		},
	}
}

// Deploy job by alias.
func deploy(c *cli.Context) {
	site, err := getJobFromAlias(c.Args())
	if err != nil {
		log.Fatalln(err)
	}

	parameters := getJobParameters(loadConfig(), site)
	buildParameters := make(map[string]string)
	buildParameters["delay"] = "0"

	if len(parameters.BooleanParameters) > 0 {
		active_boolean, negative_boolean := checkboxes("Select deploy parameters", parameters.BooleanParameters)
		for _, active_boolean_key := range active_boolean {
			buildParameters[active_boolean_key] = "true"
		}
		for _, negatgive_boolean_key := range negative_boolean {
			buildParameters[negatgive_boolean_key] = "false"
		}
	}

	if len(parameters.StringParameters) > 0 {
		scanner := bufio.NewScanner(os.Stdin)
		for _, string_parameter := range parameters.StringParameters {
			fmt.Printf(string_parameter + ": ")
			scanner.Scan()
			buildParameters[string_parameter] = scanner.Text()
		}
	}

	for choice_parameter, choice_maps := range parameters.ChoiceParameters {
		choice_result := select_choice(choice_parameter, choice_maps)
		buildParameters[choice_parameter] = choice_result
	}

	jsonString, _ := json.Marshal(buildParameters)
	fmt.Println("This will deploy", site.Alias, "with parameters", string(jsonString))
	if yesNo() {
		success, status := deployWithParameters(configuration, site, buildParameters)
		if success {
			fmt.Printf("%s deployed successfully", site.Alias)
		} else {
			fmt.Println(status)
		}
	} else {
		fmt.Println("Deploy abandoned.")
	}
}

// Set Difference: A - B
func Difference(a, b []string) (diff []string) {
	m := make(map[string]bool)

	for _, item := range b {
		m[item] = true
	}

	for _, item := range a {
		if _, ok := m[item]; !ok {
			diff = append(diff, item)
		}
	}
	return
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
	if strings.Contains(formattedUrl, configuration.JenkinsURL) {
		formattedUrl = strings.Replace(formattedUrl, configuration.JenkinsURL, "", 1)
	}
	if strings.HasPrefix(c.Args()[1], "/") {
		formattedUrl = formattedUrl[1:]
	}
	if !strings.HasSuffix(c.Args()[1], "/") {
		formattedUrl += "/"
	}
	jobUrl := formattedUrl + "api/json"

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
	log.Fatalln("Please double check your URL pattern.")

}

// Remove existing alias.
func remove(c *cli.Context) {

	site, err := getJobFromAlias(c.Args())
	if err != nil {
		log.Fatalln(err)
	}

	removed := false
	for index, job := range configuration.Jobs {
		if site == job {
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
		fmt.Printf("%s has been removed\n", site.Alias)
	} else {
		log.Fatalf("%s cannt be found\n", site.Alias)
	}

}

// Get the status of the latest build.
func status(c *cli.Context) {
	site, err := getJobFromAlias(c.Args())
	if err != nil {
		log.Fatalln(err)
	}

	buildNumber := "lastBuild"
	if len(c.Args()) > 1 {
		buildNumber = c.Args()[1]
	}

	buildUrl := site.URL + buildNumber + "/api/json?tree=fullDisplayName,id,building,result,url"
	response := getFromJenkins(configuration, buildUrl)

	var result map[string]interface{}

	err = json.Unmarshal(response, &result)
	if err != nil {
		log.Fatalln("Error when decoding build status", err)
	}

	fmt.Printf("Status for \"%s\"\n", result["fullDisplayName"])
	for key, val := range result {
		if key != "_class" && key != "fullDisplayName" {
			fmt.Printf("%s: %v\n", strings.Title(key), val)
		}
	}

}

// Helper function to extract Jenkins job object from command line arguments.
func getJobFromAlias(args []string) (config.JenkinsJob, error) {
	if len(args) < 1 {
		log.Fatalln("Please enter an alias as argument")
	}
	siteAlias := args[0]
	for _, job := range configuration.Jobs {
		if siteAlias == job.Alias {
			return job, nil
		}
	}

	return config.JenkinsJob{}, errors.New(siteAlias + ": alias cannot be found")
}

// Retrieve jenkins job parameter.
func getJobParameters(configuration config.JenkinsConfigurations, job config.JenkinsJob) config.JenkinsParameters {
	jobUrl := job.URL + "api/json?tree=property[parameterDefinitions[name,description,type,choices]]"
	response := getFromJenkins(configuration, jobUrl)
	var userParameters config.JenkinsParameters
	userParameters.ChoiceParameters = make(map[string][]string)

	var result map[string]interface{}

	err := json.Unmarshal(response, &result)
	if err != nil {
		log.Fatalln("Error when decoding Jenkins job configurations", err)
	}
	for _, val := range result["property"].([]interface{}) {
		if parameters := val.(map[string]interface{})["parameterDefinitions"]; parameters != nil {
			for _, parameter := range parameters.([]interface{}) {
				parameterName := (parameter.(map[string]interface{})["name"]).(string)
				switch parameter.(map[string]interface{})["type"] {
				case "BooleanParameterDefinition":
					userParameters.BooleanParameters = append(userParameters.BooleanParameters, parameterName)
				case "StringParameterDefinition":
					userParameters.StringParameters = append(userParameters.StringParameters, parameterName)
				case "ChoiceParameterDefinition":
					choices := parameter.(map[string]interface{})["choices"]
					for _, choice_val := range choices.([]interface{}) {
						userParameters.ChoiceParameters[parameterName] = append(userParameters.ChoiceParameters[parameterName], choice_val.(string))
					}
				}
			}
		}
	}

	return userParameters

}

// Generic request handler for Jenkins GET requests.
func getFromJenkins(configuration config.JenkinsConfigurations, endpoint string) []byte {
	fullUrl := configuration.JenkinsURL + endpoint
	req, err := http.NewRequest("GET", fullUrl, nil)
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
func deployWithParameters(configuration config.JenkinsConfigurations, job config.JenkinsJob, parameters map[string]string) (bool, string) {
	jobUrl := configuration.JenkinsURL + job.URL + "buildWithParameters"
	data := url.Values{}
	for parameter_name, parameter_value := range parameters {
		if parameter_value != "" {
			data.Add(parameter_name, parameter_value)
		}
	}

	req, _ := http.NewRequest("POST", jobUrl, strings.NewReader(data.Encode()))
	req.SetBasicAuth(configuration.JenkinsUser, configuration.JenkinsAPI)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
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
func checkboxes(label string, opts []string) ([]string, []string) {
	var res []string
	prompt := &survey.MultiSelect{
		Message: label,
		Options: opts,
	}
	err := survey.AskOne(prompt, &res)
	if err != nil {
		return nil, nil
	}

	return res, Difference(opts, res)
}

// Get a result from multiple choice.
func select_choice(label string, options []string) string {
	prompt := promptui.Select{
		Label: label,
		Items: options,
	}
	_, result, err := prompt.Run()
	if err != nil {
		log.Fatalf("Prompt failed %v\n", err)
	}
	return result
}
