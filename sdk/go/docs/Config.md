# Config

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Bus** | Pointer to **map[string]interface{}** |  | [optional] 
**AgentRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**Queue** | Pointer to **map[string]interface{}** |  | [optional] 
**MemoryManager** | Pointer to **map[string]interface{}** |  | [optional] 
**TaskRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**SessionStore** | Pointer to **map[string]interface{}** |  | [optional] 
**WorkerPool** | Pointer to **map[string]interface{}** |  | [optional] 
**SkillRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**SkillExecutor** | Pointer to **map[string]interface{}** |  | [optional] 
**TemplateRegistry** | Pointer to **map[string]interface{}** |  | [optional] 
**SelfImprove** | Pointer to **map[string]interface{}** |  | [optional] 
**TokenCache** | Pointer to **map[string]interface{}** |  | [optional] 
**SecurityChecker** | Pointer to **map[string]interface{}** |  | [optional] 
**Scheduler** | Pointer to **map[string]interface{}** |  | [optional] 
**CalendarClient** | Pointer to **map[string]interface{}** |  | [optional] 
**DaemonController** | Pointer to **map[string]interface{}** |  | [optional] 
**RuntimeManager** | Pointer to **map[string]interface{}** |  | [optional] 
**WorkingDir** | Pointer to **string** |  | [optional] 
**PidFile** | Pointer to **string** |  | [optional] 
**StateDir** | Pointer to **string** |  | [optional] 
**BinPath** | Pointer to **string** |  | [optional] 
**ProjectManager** | Pointer to **map[string]interface{}** |  | [optional] 
**PlanManager** | Pointer to **map[string]interface{}** |  | [optional] 
**PlanStore** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewConfig

`func NewConfig() *Config`

NewConfig instantiates a new Config object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewConfigWithDefaults

`func NewConfigWithDefaults() *Config`

NewConfigWithDefaults instantiates a new Config object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetBus

`func (o *Config) GetBus() map[string]interface{}`

GetBus returns the Bus field if non-nil, zero value otherwise.

### GetBusOk

`func (o *Config) GetBusOk() (*map[string]interface{}, bool)`

GetBusOk returns a tuple with the Bus field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBus

`func (o *Config) SetBus(v map[string]interface{})`

SetBus sets Bus field to given value.

### HasBus

`func (o *Config) HasBus() bool`

HasBus returns a boolean if a field has been set.

### SetBusNil

`func (o *Config) SetBusNil(b bool)`

 SetBusNil sets the value for Bus to be an explicit nil

### UnsetBus
`func (o *Config) UnsetBus()`

UnsetBus ensures that no value is present for Bus, not even an explicit nil
### GetAgentRegistry

`func (o *Config) GetAgentRegistry() map[string]interface{}`

GetAgentRegistry returns the AgentRegistry field if non-nil, zero value otherwise.

### GetAgentRegistryOk

`func (o *Config) GetAgentRegistryOk() (*map[string]interface{}, bool)`

GetAgentRegistryOk returns a tuple with the AgentRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAgentRegistry

`func (o *Config) SetAgentRegistry(v map[string]interface{})`

SetAgentRegistry sets AgentRegistry field to given value.

### HasAgentRegistry

`func (o *Config) HasAgentRegistry() bool`

HasAgentRegistry returns a boolean if a field has been set.

### SetAgentRegistryNil

`func (o *Config) SetAgentRegistryNil(b bool)`

 SetAgentRegistryNil sets the value for AgentRegistry to be an explicit nil

### UnsetAgentRegistry
`func (o *Config) UnsetAgentRegistry()`

UnsetAgentRegistry ensures that no value is present for AgentRegistry, not even an explicit nil
### GetQueue

`func (o *Config) GetQueue() map[string]interface{}`

GetQueue returns the Queue field if non-nil, zero value otherwise.

### GetQueueOk

`func (o *Config) GetQueueOk() (*map[string]interface{}, bool)`

GetQueueOk returns a tuple with the Queue field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetQueue

`func (o *Config) SetQueue(v map[string]interface{})`

SetQueue sets Queue field to given value.

### HasQueue

`func (o *Config) HasQueue() bool`

HasQueue returns a boolean if a field has been set.

### GetMemoryManager

`func (o *Config) GetMemoryManager() map[string]interface{}`

GetMemoryManager returns the MemoryManager field if non-nil, zero value otherwise.

### GetMemoryManagerOk

`func (o *Config) GetMemoryManagerOk() (*map[string]interface{}, bool)`

GetMemoryManagerOk returns a tuple with the MemoryManager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMemoryManager

`func (o *Config) SetMemoryManager(v map[string]interface{})`

SetMemoryManager sets MemoryManager field to given value.

### HasMemoryManager

`func (o *Config) HasMemoryManager() bool`

HasMemoryManager returns a boolean if a field has been set.

### SetMemoryManagerNil

`func (o *Config) SetMemoryManagerNil(b bool)`

 SetMemoryManagerNil sets the value for MemoryManager to be an explicit nil

### UnsetMemoryManager
`func (o *Config) UnsetMemoryManager()`

UnsetMemoryManager ensures that no value is present for MemoryManager, not even an explicit nil
### GetTaskRegistry

`func (o *Config) GetTaskRegistry() map[string]interface{}`

GetTaskRegistry returns the TaskRegistry field if non-nil, zero value otherwise.

### GetTaskRegistryOk

`func (o *Config) GetTaskRegistryOk() (*map[string]interface{}, bool)`

GetTaskRegistryOk returns a tuple with the TaskRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTaskRegistry

`func (o *Config) SetTaskRegistry(v map[string]interface{})`

SetTaskRegistry sets TaskRegistry field to given value.

### HasTaskRegistry

`func (o *Config) HasTaskRegistry() bool`

HasTaskRegistry returns a boolean if a field has been set.

### SetTaskRegistryNil

`func (o *Config) SetTaskRegistryNil(b bool)`

 SetTaskRegistryNil sets the value for TaskRegistry to be an explicit nil

### UnsetTaskRegistry
`func (o *Config) UnsetTaskRegistry()`

UnsetTaskRegistry ensures that no value is present for TaskRegistry, not even an explicit nil
### GetSessionStore

`func (o *Config) GetSessionStore() map[string]interface{}`

GetSessionStore returns the SessionStore field if non-nil, zero value otherwise.

### GetSessionStoreOk

`func (o *Config) GetSessionStoreOk() (*map[string]interface{}, bool)`

GetSessionStoreOk returns a tuple with the SessionStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionStore

`func (o *Config) SetSessionStore(v map[string]interface{})`

SetSessionStore sets SessionStore field to given value.

### HasSessionStore

`func (o *Config) HasSessionStore() bool`

HasSessionStore returns a boolean if a field has been set.

### GetWorkerPool

`func (o *Config) GetWorkerPool() map[string]interface{}`

GetWorkerPool returns the WorkerPool field if non-nil, zero value otherwise.

### GetWorkerPoolOk

`func (o *Config) GetWorkerPoolOk() (*map[string]interface{}, bool)`

GetWorkerPoolOk returns a tuple with the WorkerPool field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkerPool

`func (o *Config) SetWorkerPool(v map[string]interface{})`

SetWorkerPool sets WorkerPool field to given value.

### HasWorkerPool

`func (o *Config) HasWorkerPool() bool`

HasWorkerPool returns a boolean if a field has been set.

### SetWorkerPoolNil

`func (o *Config) SetWorkerPoolNil(b bool)`

 SetWorkerPoolNil sets the value for WorkerPool to be an explicit nil

### UnsetWorkerPool
`func (o *Config) UnsetWorkerPool()`

UnsetWorkerPool ensures that no value is present for WorkerPool, not even an explicit nil
### GetSkillRegistry

`func (o *Config) GetSkillRegistry() map[string]interface{}`

GetSkillRegistry returns the SkillRegistry field if non-nil, zero value otherwise.

### GetSkillRegistryOk

`func (o *Config) GetSkillRegistryOk() (*map[string]interface{}, bool)`

GetSkillRegistryOk returns a tuple with the SkillRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSkillRegistry

`func (o *Config) SetSkillRegistry(v map[string]interface{})`

SetSkillRegistry sets SkillRegistry field to given value.

### HasSkillRegistry

`func (o *Config) HasSkillRegistry() bool`

HasSkillRegistry returns a boolean if a field has been set.

### SetSkillRegistryNil

`func (o *Config) SetSkillRegistryNil(b bool)`

 SetSkillRegistryNil sets the value for SkillRegistry to be an explicit nil

### UnsetSkillRegistry
`func (o *Config) UnsetSkillRegistry()`

UnsetSkillRegistry ensures that no value is present for SkillRegistry, not even an explicit nil
### GetSkillExecutor

`func (o *Config) GetSkillExecutor() map[string]interface{}`

GetSkillExecutor returns the SkillExecutor field if non-nil, zero value otherwise.

### GetSkillExecutorOk

`func (o *Config) GetSkillExecutorOk() (*map[string]interface{}, bool)`

GetSkillExecutorOk returns a tuple with the SkillExecutor field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSkillExecutor

`func (o *Config) SetSkillExecutor(v map[string]interface{})`

SetSkillExecutor sets SkillExecutor field to given value.

### HasSkillExecutor

`func (o *Config) HasSkillExecutor() bool`

HasSkillExecutor returns a boolean if a field has been set.

### SetSkillExecutorNil

`func (o *Config) SetSkillExecutorNil(b bool)`

 SetSkillExecutorNil sets the value for SkillExecutor to be an explicit nil

### UnsetSkillExecutor
`func (o *Config) UnsetSkillExecutor()`

UnsetSkillExecutor ensures that no value is present for SkillExecutor, not even an explicit nil
### GetTemplateRegistry

`func (o *Config) GetTemplateRegistry() map[string]interface{}`

GetTemplateRegistry returns the TemplateRegistry field if non-nil, zero value otherwise.

### GetTemplateRegistryOk

`func (o *Config) GetTemplateRegistryOk() (*map[string]interface{}, bool)`

GetTemplateRegistryOk returns a tuple with the TemplateRegistry field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTemplateRegistry

`func (o *Config) SetTemplateRegistry(v map[string]interface{})`

SetTemplateRegistry sets TemplateRegistry field to given value.

### HasTemplateRegistry

`func (o *Config) HasTemplateRegistry() bool`

HasTemplateRegistry returns a boolean if a field has been set.

### SetTemplateRegistryNil

`func (o *Config) SetTemplateRegistryNil(b bool)`

 SetTemplateRegistryNil sets the value for TemplateRegistry to be an explicit nil

### UnsetTemplateRegistry
`func (o *Config) UnsetTemplateRegistry()`

UnsetTemplateRegistry ensures that no value is present for TemplateRegistry, not even an explicit nil
### GetSelfImprove

`func (o *Config) GetSelfImprove() map[string]interface{}`

GetSelfImprove returns the SelfImprove field if non-nil, zero value otherwise.

### GetSelfImproveOk

`func (o *Config) GetSelfImproveOk() (*map[string]interface{}, bool)`

GetSelfImproveOk returns a tuple with the SelfImprove field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSelfImprove

`func (o *Config) SetSelfImprove(v map[string]interface{})`

SetSelfImprove sets SelfImprove field to given value.

### HasSelfImprove

`func (o *Config) HasSelfImprove() bool`

HasSelfImprove returns a boolean if a field has been set.

### SetSelfImproveNil

`func (o *Config) SetSelfImproveNil(b bool)`

 SetSelfImproveNil sets the value for SelfImprove to be an explicit nil

### UnsetSelfImprove
`func (o *Config) UnsetSelfImprove()`

UnsetSelfImprove ensures that no value is present for SelfImprove, not even an explicit nil
### GetTokenCache

`func (o *Config) GetTokenCache() map[string]interface{}`

GetTokenCache returns the TokenCache field if non-nil, zero value otherwise.

### GetTokenCacheOk

`func (o *Config) GetTokenCacheOk() (*map[string]interface{}, bool)`

GetTokenCacheOk returns a tuple with the TokenCache field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTokenCache

`func (o *Config) SetTokenCache(v map[string]interface{})`

SetTokenCache sets TokenCache field to given value.

### HasTokenCache

`func (o *Config) HasTokenCache() bool`

HasTokenCache returns a boolean if a field has been set.

### SetTokenCacheNil

`func (o *Config) SetTokenCacheNil(b bool)`

 SetTokenCacheNil sets the value for TokenCache to be an explicit nil

### UnsetTokenCache
`func (o *Config) UnsetTokenCache()`

UnsetTokenCache ensures that no value is present for TokenCache, not even an explicit nil
### GetSecurityChecker

`func (o *Config) GetSecurityChecker() map[string]interface{}`

GetSecurityChecker returns the SecurityChecker field if non-nil, zero value otherwise.

### GetSecurityCheckerOk

`func (o *Config) GetSecurityCheckerOk() (*map[string]interface{}, bool)`

GetSecurityCheckerOk returns a tuple with the SecurityChecker field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSecurityChecker

`func (o *Config) SetSecurityChecker(v map[string]interface{})`

SetSecurityChecker sets SecurityChecker field to given value.

### HasSecurityChecker

`func (o *Config) HasSecurityChecker() bool`

HasSecurityChecker returns a boolean if a field has been set.

### SetSecurityCheckerNil

`func (o *Config) SetSecurityCheckerNil(b bool)`

 SetSecurityCheckerNil sets the value for SecurityChecker to be an explicit nil

### UnsetSecurityChecker
`func (o *Config) UnsetSecurityChecker()`

UnsetSecurityChecker ensures that no value is present for SecurityChecker, not even an explicit nil
### GetScheduler

`func (o *Config) GetScheduler() map[string]interface{}`

GetScheduler returns the Scheduler field if non-nil, zero value otherwise.

### GetSchedulerOk

`func (o *Config) GetSchedulerOk() (*map[string]interface{}, bool)`

GetSchedulerOk returns a tuple with the Scheduler field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetScheduler

`func (o *Config) SetScheduler(v map[string]interface{})`

SetScheduler sets Scheduler field to given value.

### HasScheduler

`func (o *Config) HasScheduler() bool`

HasScheduler returns a boolean if a field has been set.

### SetSchedulerNil

`func (o *Config) SetSchedulerNil(b bool)`

 SetSchedulerNil sets the value for Scheduler to be an explicit nil

### UnsetScheduler
`func (o *Config) UnsetScheduler()`

UnsetScheduler ensures that no value is present for Scheduler, not even an explicit nil
### GetCalendarClient

`func (o *Config) GetCalendarClient() map[string]interface{}`

GetCalendarClient returns the CalendarClient field if non-nil, zero value otherwise.

### GetCalendarClientOk

`func (o *Config) GetCalendarClientOk() (*map[string]interface{}, bool)`

GetCalendarClientOk returns a tuple with the CalendarClient field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCalendarClient

`func (o *Config) SetCalendarClient(v map[string]interface{})`

SetCalendarClient sets CalendarClient field to given value.

### HasCalendarClient

`func (o *Config) HasCalendarClient() bool`

HasCalendarClient returns a boolean if a field has been set.

### SetCalendarClientNil

`func (o *Config) SetCalendarClientNil(b bool)`

 SetCalendarClientNil sets the value for CalendarClient to be an explicit nil

### UnsetCalendarClient
`func (o *Config) UnsetCalendarClient()`

UnsetCalendarClient ensures that no value is present for CalendarClient, not even an explicit nil
### GetDaemonController

`func (o *Config) GetDaemonController() map[string]interface{}`

GetDaemonController returns the DaemonController field if non-nil, zero value otherwise.

### GetDaemonControllerOk

`func (o *Config) GetDaemonControllerOk() (*map[string]interface{}, bool)`

GetDaemonControllerOk returns a tuple with the DaemonController field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDaemonController

`func (o *Config) SetDaemonController(v map[string]interface{})`

SetDaemonController sets DaemonController field to given value.

### HasDaemonController

`func (o *Config) HasDaemonController() bool`

HasDaemonController returns a boolean if a field has been set.

### GetRuntimeManager

`func (o *Config) GetRuntimeManager() map[string]interface{}`

GetRuntimeManager returns the RuntimeManager field if non-nil, zero value otherwise.

### GetRuntimeManagerOk

`func (o *Config) GetRuntimeManagerOk() (*map[string]interface{}, bool)`

GetRuntimeManagerOk returns a tuple with the RuntimeManager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRuntimeManager

`func (o *Config) SetRuntimeManager(v map[string]interface{})`

SetRuntimeManager sets RuntimeManager field to given value.

### HasRuntimeManager

`func (o *Config) HasRuntimeManager() bool`

HasRuntimeManager returns a boolean if a field has been set.

### SetRuntimeManagerNil

`func (o *Config) SetRuntimeManagerNil(b bool)`

 SetRuntimeManagerNil sets the value for RuntimeManager to be an explicit nil

### UnsetRuntimeManager
`func (o *Config) UnsetRuntimeManager()`

UnsetRuntimeManager ensures that no value is present for RuntimeManager, not even an explicit nil
### GetWorkingDir

`func (o *Config) GetWorkingDir() string`

GetWorkingDir returns the WorkingDir field if non-nil, zero value otherwise.

### GetWorkingDirOk

`func (o *Config) GetWorkingDirOk() (*string, bool)`

GetWorkingDirOk returns a tuple with the WorkingDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkingDir

`func (o *Config) SetWorkingDir(v string)`

SetWorkingDir sets WorkingDir field to given value.

### HasWorkingDir

`func (o *Config) HasWorkingDir() bool`

HasWorkingDir returns a boolean if a field has been set.

### GetPidFile

`func (o *Config) GetPidFile() string`

GetPidFile returns the PidFile field if non-nil, zero value otherwise.

### GetPidFileOk

`func (o *Config) GetPidFileOk() (*string, bool)`

GetPidFileOk returns a tuple with the PidFile field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPidFile

`func (o *Config) SetPidFile(v string)`

SetPidFile sets PidFile field to given value.

### HasPidFile

`func (o *Config) HasPidFile() bool`

HasPidFile returns a boolean if a field has been set.

### GetStateDir

`func (o *Config) GetStateDir() string`

GetStateDir returns the StateDir field if non-nil, zero value otherwise.

### GetStateDirOk

`func (o *Config) GetStateDirOk() (*string, bool)`

GetStateDirOk returns a tuple with the StateDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStateDir

`func (o *Config) SetStateDir(v string)`

SetStateDir sets StateDir field to given value.

### HasStateDir

`func (o *Config) HasStateDir() bool`

HasStateDir returns a boolean if a field has been set.

### GetBinPath

`func (o *Config) GetBinPath() string`

GetBinPath returns the BinPath field if non-nil, zero value otherwise.

### GetBinPathOk

`func (o *Config) GetBinPathOk() (*string, bool)`

GetBinPathOk returns a tuple with the BinPath field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBinPath

`func (o *Config) SetBinPath(v string)`

SetBinPath sets BinPath field to given value.

### HasBinPath

`func (o *Config) HasBinPath() bool`

HasBinPath returns a boolean if a field has been set.

### GetProjectManager

`func (o *Config) GetProjectManager() map[string]interface{}`

GetProjectManager returns the ProjectManager field if non-nil, zero value otherwise.

### GetProjectManagerOk

`func (o *Config) GetProjectManagerOk() (*map[string]interface{}, bool)`

GetProjectManagerOk returns a tuple with the ProjectManager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetProjectManager

`func (o *Config) SetProjectManager(v map[string]interface{})`

SetProjectManager sets ProjectManager field to given value.

### HasProjectManager

`func (o *Config) HasProjectManager() bool`

HasProjectManager returns a boolean if a field has been set.

### SetProjectManagerNil

`func (o *Config) SetProjectManagerNil(b bool)`

 SetProjectManagerNil sets the value for ProjectManager to be an explicit nil

### UnsetProjectManager
`func (o *Config) UnsetProjectManager()`

UnsetProjectManager ensures that no value is present for ProjectManager, not even an explicit nil
### GetPlanManager

`func (o *Config) GetPlanManager() map[string]interface{}`

GetPlanManager returns the PlanManager field if non-nil, zero value otherwise.

### GetPlanManagerOk

`func (o *Config) GetPlanManagerOk() (*map[string]interface{}, bool)`

GetPlanManagerOk returns a tuple with the PlanManager field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPlanManager

`func (o *Config) SetPlanManager(v map[string]interface{})`

SetPlanManager sets PlanManager field to given value.

### HasPlanManager

`func (o *Config) HasPlanManager() bool`

HasPlanManager returns a boolean if a field has been set.

### SetPlanManagerNil

`func (o *Config) SetPlanManagerNil(b bool)`

 SetPlanManagerNil sets the value for PlanManager to be an explicit nil

### UnsetPlanManager
`func (o *Config) UnsetPlanManager()`

UnsetPlanManager ensures that no value is present for PlanManager, not even an explicit nil
### GetPlanStore

`func (o *Config) GetPlanStore() map[string]interface{}`

GetPlanStore returns the PlanStore field if non-nil, zero value otherwise.

### GetPlanStoreOk

`func (o *Config) GetPlanStoreOk() (*map[string]interface{}, bool)`

GetPlanStoreOk returns a tuple with the PlanStore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPlanStore

`func (o *Config) SetPlanStore(v map[string]interface{})`

SetPlanStore sets PlanStore field to given value.

### HasPlanStore

`func (o *Config) HasPlanStore() bool`

HasPlanStore returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


