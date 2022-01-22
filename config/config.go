package config

type JenkinsConfigurations struct {
	JenkinsURL  string
	JenkinsUser string
	JenkinsAPI  string
	Jobs        []JenkinsJob
}

type JenkinsJob struct {
	Alias string
	URL   string
}
