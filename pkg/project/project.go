package project

import (
	"sync"
	"errors"
	"github.com/sirupsen/logrus"

	"WarpCloud/walm/pkg/redis"
	"WarpCloud/walm/pkg/util/dag"
	walmerr "WarpCloud/walm/pkg/util/error"
	"fmt"
	"WarpCloud/walm/pkg/task"
	"time"
	"WarpCloud/walm/pkg/release/manager/helm/cache"
	"WarpCloud/walm/pkg/release/manager/helm"
	"WarpCloud/walm/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultSleepTimeSecond time.Duration = 1 * time.Second
	defaultTimeoutSec      int64         = 60
)

type ProjectManager struct {
	helmClient  *helm.HelmClient
	redisClient *redis.RedisClient
}

var projectManager *ProjectManager

func GetDefaultProjectManager() *ProjectManager {
	if projectManager == nil {
		projectManager = &ProjectManager{
			helmClient:  helm.GetDefaultHelmClient(),
			redisClient: redis.GetDefaultRedisClient(),
		}
	}
	return projectManager
}

func (manager *ProjectManager) ListProjects(namespace string) (*ProjectInfoList, error) {
	projectCaches, err := manager.helmClient.GetHelmCache().GetProjectCaches(namespace)
	if err != nil {
		logrus.Errorf("failed to get project caches in namespace %s : %s", namespace, err.Error())
		return nil, err
	}

	projectInfoList := &ProjectInfoList{
		Items: []*ProjectInfo{},
	}

	mux := &sync.Mutex{}
	var wg sync.WaitGroup
	for _, projectCache := range projectCaches {
		wg.Add(1)
		go func(projectCache *cache.ProjectCache) {
			defer wg.Done()
			projectInfo, err1 := manager.buildProjectInfo(projectCache)
			if err1 != nil {
				logrus.Errorf("failed to build project info from project cache of %s/%s : %s", projectCache.Namespace, projectCache.Name, err1.Error())
				err = errors.New(err1.Error())
				return
			}
			mux.Lock()
			projectInfoList.Items = append(projectInfoList.Items, projectInfo)
			mux.Unlock()
		}(projectCache)
	}

	wg.Wait()
	if err != nil {
		logrus.Errorf("failed to build project infos : %s", err.Error())
		return nil, err
	}

	projectInfoList.Num = len(projectInfoList.Items)
	return projectInfoList, nil
}

func (manager *ProjectManager) GetProjectInfo(namespace, projectName string) (*ProjectInfo, error) {
	projectCache, err := manager.helmClient.GetHelmCache().GetProjectCache(namespace, projectName)
	if err != nil {
		logrus.Errorf("failed to get project cache of %s/%s : %s", namespace, projectName, err.Error())
		return nil, err
	}

	return manager.buildProjectInfo(projectCache)
}

func (manager *ProjectManager) buildProjectInfo(projectCache *cache.ProjectCache) (projectInfo *ProjectInfo, err error) {
	taskState := projectCache.GetLatestTaskState()
	projectInfo = &ProjectInfo{
		ProjectCache: *projectCache,
		Releases:     []*release.ReleaseInfoV2{},
	}
	if taskState != nil {
		projectInfo.LatestTaskState = &task.WalmTaskState{
			TaskUUID:  taskState.TaskUUID,
			TaskName:  taskState.TaskName,
			Error:     taskState.Error,
			CreatedAt: taskState.CreatedAt,
			State:     taskState.State,
		}
	}

	projectInfo.Releases, err = manager.helmClient.ListReleasesByLabels(projectCache.Namespace, &v1.LabelSelector{MatchLabels: map[string]string{cache.ProjectNameLabelKey: projectCache.Name}})
	if err != nil {
		return nil, err
	}

	if taskState == nil || taskState.TaskName == "" {
		projectInfo.Ready, projectInfo.Message = isProjectReadyByReleases(projectInfo.Releases)

	} else if taskState.IsSuccess() {
		projectInfo.Ready, projectInfo.Message = isProjectReadyByReleases(projectInfo.Releases)
	} else if taskState.IsFailure() {
		projectInfo.Message = fmt.Sprintf("the project latest task %s-%s failed : %s", projectCache.LatestTaskSignature.Name, projectCache.LatestTaskSignature.UUID, taskState.Error)
	} else {
		projectInfo.Message = fmt.Sprintf("please wait for the project latest task %s-%s finished", projectCache.LatestTaskSignature.Name, projectCache.LatestTaskSignature.UUID)
	}
	return
}

func isProjectReadyByReleases(releases []*release.ReleaseInfoV2) (ready bool, message string) {
	if len(releases) > 0 {
		ready = true
		for _, releaseInfo := range releases {
			if !releaseInfo.Ready {
				ready = false
				message = releaseInfo.Message
				break
			}
		}
	} else {
		message = "no release can be found"
	}
	return
}

func (manager *ProjectManager) validateProjectTask(namespace, name string, allowProjectNotExist bool) (projectCache *cache.ProjectCache, err error) {
	projectCache, err = manager.helmClient.GetHelmCache().GetProjectCache(namespace, name)
	if err != nil {
		if !walmerr.IsNotFoundError(err) {
			logrus.Errorf("failed to get project cache : %s", err.Error())
			return
		} else if !allowProjectNotExist {
			return
		} else {
			err = nil
		}
	} else {
		if !projectCache.IsLatestTaskFinishedOrTimeout() {
			err = fmt.Errorf("please wait for the project latest task %s-%s finished or timeout", projectCache.LatestTaskSignature.Name, projectCache.LatestTaskSignature.UUID)
			logrus.Error(err.Error())
			return
		}
	}
	return
}

func (manager *ProjectManager) CreateProject(namespace string, project string, projectParams *ProjectParams, async bool, timeoutSec int64) error {
	if len(projectParams.Releases) == 0 {
		return errors.New("project releases can not be empty")
	}

	if timeoutSec == 0 {
		timeoutSec = defaultTimeoutSec
	}

	oldProjectCache, err := manager.validateProjectTask(namespace, project, true)
	if err != nil {
		logrus.Errorf("failed to validate project task : %s", err.Error())
		return err
	}

	createProjectTaskSig, err := SendCreateProjectTask(&CreateProjectTaskArgs{
		Name:          project,
		Namespace:     namespace,
		ProjectParams: projectParams,
	})
	if err != nil {
		logrus.Errorf("failed to send create project %s/%s task : %s", namespace, project, err.Error())
		return err
	}

	projectCache := &cache.ProjectCache{
		Namespace:            namespace,
		Name:                 project,
		LatestTaskSignature:  createProjectTaskSig,
		LatestTaskTimeoutSec: timeoutSec,
	}
	err = manager.helmClient.GetHelmCache().CreateOrUpdateProjectCache(projectCache)
	if err != nil {
		logrus.Errorf("failed to set project cache of %s/%s to redis: %s", namespace, project, err.Error())
		return err
	}

	if oldProjectCache != nil {
		err = task.GetDefaultTaskManager().PurgeTaskState(oldProjectCache.GetLatestTaskSignature())
		if err != nil {
			logrus.Warnf("failed to purge task state : %s", err.Error())
		}
	}

	if !async {
		asyncResult := task.GetDefaultTaskManager().NewAsyncResult(projectCache.GetLatestTaskSignature())
		_, err = asyncResult.GetWithTimeout(time.Duration(timeoutSec)*time.Second, defaultSleepTimeSecond)
		if err != nil {
			logrus.Errorf("failed to create project  %s/%s: %s", namespace, project, err.Error())
			return err
		}

	}
	logrus.Infof("succeed to create project %s/%s", namespace, project)
	return nil
}

func (manager *ProjectManager) DeleteProject(namespace string, project string, async bool, timeoutSec int64, deletePvcs bool) error {
	oldProjectCache, err := manager.validateProjectTask(namespace, project, false)
	if err != nil {
		if walmerr.IsNotFoundError(err) {
			logrus.Warnf("project %s/%s is not found", namespace, project)
			return nil
		}
		logrus.Errorf("failed to validate project job : %s", err.Error())
		return err
	}

	if timeoutSec == 0 {
		timeoutSec = defaultTimeoutSec
	}

	deleteProjectTaskSig, err := SendDeleteProjectTask(&DeleteProjectTaskArgs{
		Name:       project,
		Namespace:  namespace,
		DeletePvcs: deletePvcs,
	})
	if err != nil {
		logrus.Errorf("failed to send delete project %s/%s task : %s", namespace, project, err.Error())
		return err
	}

	projectCache := &cache.ProjectCache{
		Namespace:            namespace,
		Name:                 project,
		LatestTaskSignature:  deleteProjectTaskSig,
		LatestTaskTimeoutSec: timeoutSec,
	}
	err = manager.helmClient.GetHelmCache().CreateOrUpdateProjectCache(projectCache)
	if err != nil {
		logrus.Errorf("failed to set project cache of %s/%s to redis: %s", namespace, project, err.Error())
		return err
	}

	if oldProjectCache != nil {
		err = task.GetDefaultTaskManager().PurgeTaskState(oldProjectCache.GetLatestTaskSignature())
		if err != nil {
			logrus.Warnf("failed to purge task state : %s", err.Error())
		}
	}

	if !async {
		asyncResult := task.GetDefaultTaskManager().NewAsyncResult(projectCache.GetLatestTaskSignature())
		_, err = asyncResult.GetWithTimeout(time.Duration(timeoutSec)*time.Second, defaultSleepTimeSecond)
		if err != nil {
			logrus.Errorf("failed to delete project  %s/%s : %s", namespace, project, err.Error())
			return err
		}
	}
	logrus.Infof("succeed to delete project %s/%s", namespace, project)

	return nil
}

func (manager *ProjectManager) AddReleaseInProject(namespace string, projectName string, releaseParams *release.ReleaseRequestV2, async bool, timeoutSec int64) error {
	return manager.AddReleasesInProject(namespace, projectName, &ProjectParams{Releases: []*release.ReleaseRequestV2{releaseParams}}, async, timeoutSec)
}

func (manager *ProjectManager) UpgradeReleaseInProject(namespace string, projectName string, releaseParams *release.ReleaseRequestV2, async bool, timeoutSec int64) error {
	oldProjectCache, err := manager.validateProjectTask(namespace, projectName, false)
	if err != nil {
		if walmerr.IsNotFoundError(err) {
			logrus.Warnf("project %s/%s is not found", namespace, projectName)
			return nil
		}
		logrus.Errorf("failed to validate project job : %s", err.Error())
		return err
	}

	projectInfo, err := manager.buildProjectInfo(oldProjectCache)
	if err != nil {
		logrus.Errorf("failed to build project info : %s", err.Error())
		return err
	}

	releaseExistsInProject := false
	for _, releaseInfo := range projectInfo.Releases {
		if releaseInfo.Name == releaseParams.Name {
			releaseExistsInProject = true
			break
		}
	}

	if !releaseExistsInProject {
		err = fmt.Errorf("release %s is not found in project %s", releaseParams.Name, projectName)
		logrus.Error(err.Error())
		return err
	}

	if timeoutSec == 0 {
		timeoutSec = defaultTimeoutSec
	}

	upgradeReleaseTaskSig, err := SendUpgradeReleaseTask(&UpgradeReleaseTaskArgs{
		Namespace:     namespace,
		ProjectName:   projectName,
		ReleaseParams: releaseParams,
	})
	if err != nil {
		logrus.Errorf("failed to send upgrade release %s in project %s/%s task : %s", releaseParams.Name, namespace, projectName, err.Error())
		return err
	}

	projectCache := &cache.ProjectCache{
		Namespace:            namespace,
		Name:                 projectName,
		LatestTaskSignature:  upgradeReleaseTaskSig,
		LatestTaskTimeoutSec: timeoutSec,
	}
	err = manager.helmClient.GetHelmCache().CreateOrUpdateProjectCache(projectCache)
	if err != nil {
		logrus.Errorf("failed to set project cache of %s/%s to redis: %s", namespace, projectName, err.Error())
		return err
	}

	if oldProjectCache != nil {
		err = task.GetDefaultTaskManager().PurgeTaskState(oldProjectCache.GetLatestTaskSignature())
		if err != nil {
			logrus.Warnf("failed to purge task state : %s", err.Error())
		}
	}

	if !async {
		asyncResult := task.GetDefaultTaskManager().NewAsyncResult(projectCache.GetLatestTaskSignature())
		_, err = asyncResult.GetWithTimeout(time.Duration(timeoutSec)*time.Second, defaultSleepTimeSecond)
		if err != nil {
			logrus.Errorf("failed to upgrade release %s in project %s/%s : %s", releaseParams.Name, namespace, projectName, err.Error())
			return err
		}
	}
	logrus.Infof("succeed to upgrade release %s in project %s/%s", releaseParams.Name, namespace, projectName)

	return nil
}

func (manager *ProjectManager) RemoveReleaseInProject(namespace, projectName, releaseName string, async bool, timeoutSec int64, deletePvcs bool) error {
	oldProjectCache, err := manager.validateProjectTask(namespace, projectName, false)
	if err != nil {
		if walmerr.IsNotFoundError(err) {
			logrus.Warnf("project %s/%s is not found", namespace, projectName)
			return nil
		}
		logrus.Errorf("failed to validate project job : %s", err.Error())
		return err
	}

	projectInfo, err := manager.buildProjectInfo(oldProjectCache)
	if err != nil {
		logrus.Errorf("failed to build project info : %s", err.Error())
		return err
	}

	releaseExistsInProject := false
	for _, releaseInfo := range projectInfo.Releases {
		if releaseInfo.Name == releaseName {
			releaseExistsInProject = true
			break
		}
	}

	if !releaseExistsInProject {
		logrus.Warnf("release %s is not found in project %s", releaseName, projectName)
		return nil
	}

	if timeoutSec == 0 {
		timeoutSec = defaultTimeoutSec
	}

	removeReleaseTaskSig, err := SendRemoveReleaseTask(&RemoveReleaseTaskArgs{
		Namespace:   namespace,
		Name:        projectName,
		ReleaseName: releaseName,
		DeletePvcs:  deletePvcs,
	})
	if err != nil {
		logrus.Errorf("failed to send remove release %s in project %s/%s task : %s", releaseName, namespace, projectName, err.Error())
		return err
	}

	projectCache := &cache.ProjectCache{
		Namespace:            namespace,
		Name:                 projectName,
		LatestTaskSignature:  removeReleaseTaskSig,
		LatestTaskTimeoutSec: timeoutSec,
	}
	err = manager.helmClient.GetHelmCache().CreateOrUpdateProjectCache(projectCache)
	if err != nil {
		logrus.Errorf("failed to set project cache of %s/%s to redis: %s", namespace, projectName, err.Error())
		return err
	}

	if oldProjectCache != nil {
		err = task.GetDefaultTaskManager().PurgeTaskState(oldProjectCache.GetLatestTaskSignature())
		if err != nil {
			logrus.Warnf("failed to purge task state : %s", err.Error())
		}
	}

	if !async {
		asyncResult := task.GetDefaultTaskManager().NewAsyncResult(projectCache.GetLatestTaskSignature())
		_, err = asyncResult.GetWithTimeout(time.Duration(timeoutSec)*time.Second, defaultSleepTimeSecond)
		if err != nil {
			logrus.Errorf("failed to remove release %s in project %s/%s : %s", releaseName, namespace, projectName, err.Error())
			return err
		}
	}
	logrus.Infof("succeed to remove release %s in project %s/%s", releaseName, namespace, projectName)

	return nil
}

func (manager *ProjectManager) brainFuckRuntimeDepParse(projectInfo *ProjectInfo, releaseParams *release.ReleaseRequestV2, isRemove bool) ([]*release.ReleaseRequestV2, error) {
	var g dag.AcyclicGraph
	affectReleases := make([]*release.ReleaseRequestV2, 0)

	// init node
	for _, helmRelease := range projectInfo.Releases {
		g.Add(helmRelease.Name)
	}

	// init edge
	for _, helmRelease := range projectInfo.Releases {
		for _, v := range helmRelease.Dependencies {
			g.Connect(dag.BasicEdge(helmRelease.Name, v))
		}
	}

	if !isRemove {
		g.Add(releaseParams.Name)
		for _, helmRelease := range projectInfo.Releases {
			subCharts, err := manager.helmClient.GetAutoDependencies(helmRelease.RepoName, helmRelease.ChartName, helmRelease.ChartVersion)
			if err != nil {
				return nil, err
			}
			for _, subChartName := range subCharts {
				_, ok := helmRelease.Dependencies[subChartName]
				if subChartName == releaseParams.ChartName && !ok {
					g.Connect(dag.BasicEdge(helmRelease.Name, releaseParams.Name))
				}
			}
		}
		releaseSubCharts, err := manager.helmClient.GetAutoDependencies(releaseParams.RepoName, releaseParams.ChartName, releaseParams.ChartVersion)
		if err != nil {
			return nil, err
		}
		for _, releaseSubChart := range releaseSubCharts {
			_, ok := releaseParams.Dependencies[releaseSubChart]
			if ok {
				continue
			}
			for _, helmRelease := range projectInfo.Releases {
				if releaseSubChart == helmRelease.ChartName {
					g.Connect(dag.BasicEdge(releaseParams.Name, helmRelease.Name))
				}
			}
		}
		logrus.Infof("add %s Modify UpperStream Release %+v deps %s\n", releaseParams.Name, g.UpEdges(releaseParams.Name).List(), releaseParams.Name)
		for _, upperReleaseName := range g.UpEdges(releaseParams.Name).List() {
			upperRelease := buildReleaseRequest(projectInfo, upperReleaseName.(string))
			if upperRelease == nil {
				continue
			}
			_, ok := upperRelease.Dependencies[releaseParams.ChartName]
			if !ok {
				if upperRelease.Dependencies == nil {
					upperRelease.Dependencies = map[string]string{}
				}
				upperRelease.Dependencies[releaseParams.ChartName] = releaseParams.Name
			}
			affectReleases = append(affectReleases, upperRelease)
		}
		logrus.Infof("add %s release add more %+v deps. current %+v\n",
			releaseParams.Name, g.DownEdges(releaseParams.Name).List(), releaseParams.Dependencies)
		for _, downReleaseName := range g.DownEdges(releaseParams.Name).List() {
			downRelease := buildReleaseRequest(projectInfo, downReleaseName.(string))
			if downRelease == nil {
				continue
			}
			_, ok := releaseParams.Dependencies[downRelease.ChartName]
			if !ok {
				if releaseParams.Dependencies == nil {
					releaseParams.Dependencies = make(map[string]string)
				}
				releaseParams.Dependencies[downRelease.ChartName] = downRelease.Name
				logrus.Infof("RuntimeDepParse release %s Dependencies %+v\n", releaseParams.Name, releaseParams.Dependencies)
			}
		}
	} else {
		logrus.Infof("%+v are depending on %s", g.UpEdges(releaseParams.Name).List(), releaseParams.Name)
		for _, upperReleaseName := range g.UpEdges(releaseParams.Name).List() {
			upperRelease := buildReleaseRequest(projectInfo, upperReleaseName.(string))
			if upperRelease == nil {
				continue
			}

			helm.DeleteReleaseDependency(upperRelease.Dependencies, releaseParams.ChartName)
			affectReleases = append(affectReleases, upperRelease)
		}
	}

	return affectReleases, nil
}

func (manager *ProjectManager) brainFuckChartDepParse(projectParams *ProjectParams) ([]*release.ReleaseRequestV2, error) {
	projectParamsMap := make(map[string]*release.ReleaseRequestV2)
	releaseParsed := make([]*release.ReleaseRequestV2, 0)
	var g dag.AcyclicGraph

	for _, releaseInfo := range projectParams.Releases {
		projectParamsMap[releaseInfo.ChartName] = releaseInfo
	}

	// init node
	for _, helmRelease := range projectParams.Releases {
		g.Add(helmRelease)
	}

	// init edge
	for _, helmRelease := range projectParams.Releases {
		subCharts, err := manager.helmClient.GetAutoDependencies(helmRelease.RepoName, helmRelease.ChartName, helmRelease.ChartVersion)
		if err != nil {
			return nil, err
		}

		for _, subChartName := range subCharts {
			_, ok := projectParamsMap[subChartName]
			_, ok2 := helmRelease.Dependencies[subChartName]
			if ok && !ok2 {
				g.Connect(dag.BasicEdge(helmRelease, projectParamsMap[subChartName]))
			}
		}
	}

	_, err := g.Root()
	if err != nil {
		return nil, err
	}

	var lock sync.Mutex
	err = g.Walk(func(v dag.Vertex) error {
		lock.Lock()
		defer lock.Unlock()
		releaseRequest := v.(*release.ReleaseRequestV2)
		for _, dv := range g.DownEdges(releaseRequest).List() {
			releaseInfo := dv.(*release.ReleaseRequestV2)
			releaseRequest.Dependencies[releaseInfo.ChartName] = releaseInfo.Name
		}
		releaseParsed = append(releaseParsed, releaseRequest)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return releaseParsed, nil
}

func (manager *ProjectManager) AddReleasesInProject(namespace string, projectName string, projectParams *ProjectParams, async bool, timeoutSec int64) error {
	if len(projectParams.Releases) == 0 {
		return errors.New("project releases can not be empty")
	}

	oldProjectCache, err := manager.validateProjectTask(namespace, projectName, true)
	if err != nil {
		logrus.Errorf("failed to validate project job : %s", err.Error())
		return err
	}

	if timeoutSec == 0 {
		timeoutSec = defaultTimeoutSec
	}

	addReleaseTaskSig, err := SendAddReleaseTask(&AddReleaseTaskArgs{
		Namespace:     namespace,
		Name:          projectName,
		ProjectParams: projectParams,
	})
	if err != nil {
		logrus.Errorf("failed to send add releases in project %s/%s task : %s", namespace, projectName, err.Error())
		return err
	}

	projectCache := &cache.ProjectCache{
		Namespace:            namespace,
		Name:                 projectName,
		LatestTaskSignature:  addReleaseTaskSig,
		LatestTaskTimeoutSec: timeoutSec,
	}
	err = manager.helmClient.GetHelmCache().CreateOrUpdateProjectCache(projectCache)
	if err != nil {
		logrus.Errorf("failed to set project cache of %s/%s to redis: %s", namespace, projectName, err.Error())
		return err
	}

	if oldProjectCache != nil {
		err = task.GetDefaultTaskManager().PurgeTaskState(oldProjectCache.GetLatestTaskSignature())
		if err != nil {
			logrus.Warnf("failed to purge task state : %s", err.Error())
		}
	}

	if !async {
		asyncResult := task.GetDefaultTaskManager().NewAsyncResult(projectCache.GetLatestTaskSignature())
		_, err = asyncResult.GetWithTimeout(time.Duration(timeoutSec)*time.Second, defaultSleepTimeSecond)
		if err != nil {
			logrus.Errorf("failed to add releases in project %s/%s : %s", namespace, projectName, err.Error())
			return err
		}
	}
	logrus.Infof("succeed to add releases in project %s/%s", namespace, projectName)

	return nil
}
