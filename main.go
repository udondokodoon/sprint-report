package main

import (
  "bytes"
	"encoding/json"
	"fmt"
	"github.com/andygrunwald/go-jira"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// [com.atlassian.greenhopper.service.sprint.Sprint@71f556[id=197,rapidViewId=38,state=ACTIVE,name=S54 I do not seek, I find.,startDate=2016-06-21T09:52:08.106+09:00,endDate=2016-06-29T09:52:00.000+09:00,completeDate=<null>,sequence=156]]

const timeFormat = "2006-01-02T15:04:05"

type Config struct {
	Jira  JiraConfig  `json:"jira"`
	Slack SlackConfig `json:"slack"`
}

type JiraConfig struct {
	URL         string `json:"url"`
	ProjectName string `json:"project"`
	UserName    string `json:"user"`
	UserPass    string `json:"password"`
}

type SlackConfig struct {
	IncomingWebHook string `json:"incomingWebHook"`
	Icon            string `json:"icon"`
	Channel         string `json:"channel"`
}

type SlackMessage struct {
	Text       string `json:"text"`
	Username   string `json:"username"`
	Icon_emoji string `json:"icon_emoji"`
	Icon_url   string `json:"icon_url"`
	Channel    string `json:"channel"`
}

func (s *SlackMessage) Post(incomingWebHookURL string) {
	params, _ := json.Marshal(s)
	res, _ := http.PostForm(
		incomingWebHookURL,
		url.Values{"payload": {string(params)}},
	)

	body, _ := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	log.Println(string(body))
}

type Sprint struct {
	ID        int
	Name      string
	State     string
	StartDate time.Time
	EndDate   time.Time
}

func NewSprint(s string) *Sprint {
	t := strings.Split(s, "[")
	t = strings.Split(t[2], "]")
	v := strings.Split(t[0], ",")

	m := make(map[string]string)
	for i := range v {
		n := strings.Split(v[i], "=")
		if len(n) < 2 {
			continue
		}
		m[n[0]] = n[1]
	}
	id, _ := strconv.Atoi(m["id"])
	name := m["name"]
	state := m["state"]
	startDate, _ := time.Parse(timeFormat, m["startDate"][0:19])
	endDate, _ := time.Parse(timeFormat, m["endDate"][0:19])
	sprint := &Sprint{id, name, state, startDate, endDate}
	return sprint
}

func (s *Sprint) GetRemainingDays() int {
	d := int(s.EndDate.Sub(s.StartDate).Hours() / 24)
	r := 0
	for i := 0; i <= d; i++ {
		k := s.StartDate.Add(time.Duration(i) * time.Hour * 24)
		w := k.Weekday()
		if w == time.Sunday || w == time.Saturday {
			continue
		}
		r++
	}
	return r
}

type SprintInfo struct {
  StoriesInProgress []*SprintStory
  Sum float64
  Avg float64
  Statuses map[string]*SprintStatus
}

type SprintStory struct {
  Story string
  Sp float64
}

type SprintStatus struct {
  Status string
  Sp float64
}

func readConfigFile(filename string) (*Config, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	json.Unmarshal(file, &config)
	return &config, nil
}

type V struct {

}

func _main() {
	tmpl, err := template.ParseFiles("message.tmpl")
	if err != nil {
		log.Fatal(err)
	}
	tmpl.Execute(os.Stdout, map[string]string{"template": "HOGEHOGE"})
}

func main() {
	config, err := readConfigFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

  tmpl, err := template.ParseFiles("message.tmpl")
  if err != nil {
    log.Fatal(err)
  }

	jiraClient, err := jira.NewClient(nil, config.Jira.URL)
	if err != nil {
		log.Fatal(err)
	}

	res, err := jiraClient.Authentication.AcquireSessionCookie(config.Jira.UserName, config.Jira.UserPass)
	if err != nil {
		log.Printf("Result: %v\n", res)
		log.Fatal(err)
	}

	opts := jira.SearchOptions{0, 1000}
	jql := fmt.Sprintf("project = %s AND sprint in openSprints() and status != 保留 and assignee = %s", config.Jira.ProjectName, config.Jira.UserName)
	issues, res2, err := jiraClient.Issue.Search(jql, &opts)
	if err != nil {
		log.Printf("Response: %v\n", res2)
		log.Fatal(err)
	}

	status := make(map[string]float64)
	var sprint *Sprint

  info := &SprintInfo{[]*SprintStory{}, 0, 0, map[string]*SprintStatus{}}
	sum := 0.0
	for _, issue := range issues {
		/*
			fmt.Printf("%s: %+v\n", issue.Key, issue.Fields.Summary)
			fmt.Printf("Type: %s: %+v\n", issue.Key, issue.Fields.Type.Name)
			fmt.Printf("Priority: %s: %+v\n", issue.Key, issue.Fields.Priority.Name)
			fmt.Printf("Status: %s: %+v\n", issue.Key, issue.Fields.Status.Name)
		*/

		customFields, _, err := jiraClient.Issue.GetCustomFields(issue.ID)
		if err != nil {
			continue
		}
		s := NewSprint(customFields["customfield_10006"])
		//log.Printf("%v\n", customFields["customfield_10006"])
		//log.Printf("%s\n", s.Name)
		sp, _ := strconv.ParseFloat(customFields["customfield_10004"], 64)
		sum += sp
    statusName := issue.Fields.Status.Name
    if info.Statuses[statusName] == nil {
      info.Statuses[statusName] = &SprintStatus{statusName, 0}
    }
    info.Statuses[statusName].Sp += sp;
		status[issue.Fields.Status.Name] += sp
		if issue.Fields.Status.Name == "進行中" {
      info.StoriesInProgress = append(info.StoriesInProgress, &SprintStory{issue.Fields.Summary, sp})
			//storiesInProgress = append(storiesInProgress, issue.Fields.Summary+": "+customFields["customfield_10004"])
		}

		if s.State != "ACTIVE" {
			continue
		}
		sprint = s
	}

  info.Sum = sum
  info.Avg = sum/float64(sprint.GetRemainingDays())
  buf := &bytes.Buffer{}
  tmpl.Execute(buf, info)
  log.Println(buf.String());
	msg := &SlackMessage{buf.String(), "jira-task", "", config.Slack.Icon, config.Slack.Channel}
	msg.Post(config.Slack.IncomingWebHook)
}
