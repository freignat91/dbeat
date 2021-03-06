package beater

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/freignat91/dbeat/config"
)

const (
	defaultTimeOut   = 30 * time.Second
	dockerAPIVersion = "1.24"
)

//ContainerData data
type ContainerData struct {
	name             string
	ID               string
	shortName        string
	serviceName      string
	serviceID        string
	stackName        string
	taskID           string
	nodeID           string
	role             string
	pid              int
	state            string
	health           string
	customADILabel   string
	logsStream       io.ReadCloser
	logsReadError    bool
	metricsStream    io.ReadCloser
	metricsReadError bool
	previousIOStats  *IOStats
	previousNetStats *NetStats
	lastDateSaveTime time.Time
}

//AgentStart Connect to docker engine, get initial containers list and start the agent
func (a *dbeat) start(config *config.Config) error {
	// Connection to Docker
	os.MkdirAll(containersDateDir, 0666)
	defaultHeaders := map[string]string{"User-Agent": "dbeat"}
	cli, err := client.NewClient(config.DockerURL, dockerAPIVersion, nil, defaultHeaders)
	if err != nil {
		return err
	}
	a.dockerClient = cli
	fmt.Println("Connected to Docker-engine")

	fmt.Println("Extracting containers list...")
	a.containers = make(map[string]*ContainerData)
	ContainerListOptions := types.ContainerListOptions{All: true}
	containers, err := a.dockerClient.ContainerList(context.Background(), ContainerListOptions)
	if err != nil {
		return err
	}
	for _, cont := range containers {
		a.addContainer(cont.ID)
	}
	a.lastUpdate = time.Now()
	fmt.Println("done")
	return nil
}

//starts logs and metrics stream of eech new started container
func (a *dbeat) tick() {
	if a.config.Logs {
		if a.config.Logs {
			a.updateLogsStream()
		}
		if a.config.Memory || a.config.Net || a.config.IO || a.config.CPU {
			a.updateMetricsStream()
		}
		a.updateEventsStream()
	}
}

//Verify if the event stream is working, if not start it
func (a *dbeat) updateEventsStream() {
	if !a.eventStreamReading {
		fmt.Println("Opening docker events stream...")
		args := filters.NewArgs()
		args.Add("type", "container")
		args.Add("event", "die")
		args.Add("event", "stop")
		args.Add("event", "destroy")
		args.Add("event", "kill")
		args.Add("event", "create")
		args.Add("event", "start")
		eventsOptions := types.EventsOptions{Filters: args}
		stream, err := a.dockerClient.Events(context.Background(), eventsOptions)
		a.startEventStream(stream, err)
	}
}

// Start and read the docker event stream and update container list accordingly
func (a *dbeat) startEventStream(stream <-chan events.Message, errs <-chan error) {
	a.eventStreamReading = true
	fmt.Println("start events stream reader")
	go func() {
		for {
			select {
			case err := <-errs:
				if err != nil {
					fmt.Printf("Error reading event: %v\n", err)
					a.eventStreamReading = false
					return
				}
			case event := <-stream:
				fmt.Printf("Docker event: action=%s containerId=%s\n", event.Action, event.Actor.ID)
				a.updateContainerMap(event.Action, event.Actor.ID)
			}
		}
	}()
}

//Update containers list concidering event action and event containerId
func (a *dbeat) updateContainerMap(action string, containerID string) {
	if action == "start" {
		a.addContainer(containerID)
	} else if action == "destroy" || action == "die" || action == "kill" || action == "stop" {
		go func() {
			time.Sleep(5 * time.Second)
			a.removeContainer(containerID)
		}()
	}
}

//add a container to the main container map and retrieve some container information
func (a *dbeat) addContainer(ID string) {
	_, ok := a.containers[ID]
	if !ok {
		inspect, err := a.dockerClient.ContainerInspect(context.Background(), ID)
		if err == nil {
			data := ContainerData{
				ID:            ID,
				name:          inspect.Name,
				state:         inspect.State.Status,
				pid:           inspect.State.Pid,
				health:        "",
				logsStream:    nil,
				logsReadError: false,
			}
			labels := inspect.Config.Labels
			//data.serviceName = a.getMapValue(labels, "com.docker.swarm.service.name")
			data.serviceName = strings.TrimPrefix(labels["com.docker.swarm.service.name"], labels["com.docker.stack.namespace"]+"_")
			if data.serviceName == "" {
				data.serviceName = "noService"
			}
			data.shortName = fmt.Sprintf("%s_%d", data.serviceName, data.pid)
			data.serviceID = a.getMapValue(labels, "com.docker.swarm.service.id")
			data.taskID = a.getMapValue(labels, "com.docker.swarm.task.id")
			data.nodeID = a.getMapValue(labels, "com.docker.swarm.node.id")
			data.stackName = a.getMapValue(labels, "com.docker.stack.namespace")
			if data.stackName == "" {
				data.stackName = "noStack"
			}
			fmt.Printf("custom config: %s\n", a.config.CustomADILabelName)
			fmt.Printf("labels: %v\n", labels)
			if a.config.CustomADILabelName != "" {
				fmt.Printf("found custom label: %s\n", a.getMapValue(labels, a.config.CustomADILabelName))
				data.customADILabel = a.getMapValue(labels, a.config.CustomADILabelName)
			}
			data.role = a.getMapValue(labels, "io.amp.role")
			if inspect.State.Health != nil {
				data.health = inspect.State.Health.Status
			}
			if data.role == "infrastructure" {
				fmt.Printf("add infrastructure container  %s\n", data.name)
			} else {
				fmt.Printf("add user container %s, stack=%s service=%s\n", data.name, data.stackName, data.serviceName)
			}
			a.containers[ID] = &data
		} else {
			fmt.Printf("Container inspect error: %v\n", err)
		}
	}
}

//Suppress a container from the main container map
func (a *dbeat) removeContainer(ID string) {
	data, ok := a.containers[ID]
	if ok {
		fmt.Println("remove container", data.name)
		delete(a.containers, ID)
	}
	os.Remove(path.Join(containersDateDir, ID))
}

//Update container status and health
func (a *dbeat) updateContainer(ID string) {
	data, ok := a.containers[ID]
	if ok {
		inspect, err := a.dockerClient.ContainerInspect(context.Background(), ID)
		if err == nil {
			//labels = inspect.Config.Labels
			data.state = inspect.State.Status
			data.health = ""
			if inspect.State.Health != nil {
				data.health = inspect.State.Health.Status
			}
			fmt.Println("update container", data.name)
		} else {
			fmt.Printf("Container %s inspect error: %v\n", data.name, err)
		}
	}
}

func (a *dbeat) getMapValue(labelMap map[string]string, name string) string {
	if val, exist := labelMap[name]; exist {
		//todo
		return val
	}
	return ""
}

// Close dbeat ressources
func (a *dbeat) Close() {
	a.closeLogsStreams()
	a.closeMetricsStreams()
	a.dockerClient.Close()
}
