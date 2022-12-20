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

type JenkinsParameters struct {
	BooleanParameters []string
	StringParameters  []string
	ChoiceParameters  map[string][]string
}
