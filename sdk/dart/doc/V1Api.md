# meept_client.api.V1Api

## Load the API package
```dart
import 'package:meept_client/api.dart';
```

All URIs are relative to *http://localhost:8081*

Method | HTTP request | Description
------------- | ------------- | -------------
[**apiV1BusPublishPost**](V1Api.md#apiv1buspublishpost) | **POST** /api/v1/bus/publish | s.handleBusPublish
[**apiV1BusStatsGet**](V1Api.md#apiv1busstatsget) | **GET** /api/v1/bus/stats | s.handleBusStats
[**apiV1CacheClearPost**](V1Api.md#apiv1cacheclearpost) | **POST** /api/v1/cache/clear | s.handleCacheClear
[**apiV1CacheInspectGet**](V1Api.md#apiv1cacheinspectget) | **GET** /api/v1/cache/inspect | s.handleCacheInspect
[**apiV1CacheInvalidatePost**](V1Api.md#apiv1cacheinvalidatepost) | **POST** /api/v1/cache/invalidate | s.handleCacheInvalidate
[**apiV1CacheStatsGet**](V1Api.md#apiv1cachestatsget) | **GET** /api/v1/cache/stats | s.handleCacheStats
[**apiV1CalendarEventsGet**](V1Api.md#apiv1calendareventsget) | **GET** /api/v1/calendar/events | s.handleCalendarList
[**apiV1CalendarEventsIdGet**](V1Api.md#apiv1calendareventsidget) | **GET** /api/v1/calendar/events/{id} | s.handleCalendarGet
[**apiV1CalendarQuickaddPost**](V1Api.md#apiv1calendarquickaddpost) | **POST** /api/v1/calendar/quickadd | s.handleCalendarQuickAdd
[**apiV1CalendarTodayGet**](V1Api.md#apiv1calendartodayget) | **GET** /api/v1/calendar/today | s.handleCalendarToday
[**apiV1CalendarUpcomingGet**](V1Api.md#apiv1calendarupcomingget) | **GET** /api/v1/calendar/upcoming | s.handleCalendarUpcoming
[**apiV1ChatPost**](V1Api.md#apiv1chatpost) | **POST** /api/v1/chat | s.handleChat
[**apiV1ChatQueueIdGet**](V1Api.md#apiv1chatqueueidget) | **GET** /api/v1/chat/queue/{id} | s.handleChatQueueStatus
[**apiV1ChatStreamGet**](V1Api.md#apiv1chatstreamget) | **GET** /api/v1/chat/stream | s.handleChatStream
[**apiV1ChatWithAgentPost**](V1Api.md#apiv1chatwithagentpost) | **POST** /api/v1/chat/with-agent | s.handleChatWithAgent
[**apiV1ConfigAgentsGet**](V1Api.md#apiv1configagentsget) | **GET** /api/v1/config/agents | s.handleListAgents
[**apiV1ConfigAgentsIdPost**](V1Api.md#apiv1configagentsidpost) | **POST** /api/v1/config/agents/{id} | s.handleSaveAgent
[**apiV1ConfigClientPost**](V1Api.md#apiv1configclientpost) | **POST** /api/v1/config/client | s.handleSaveClientConfig
[**apiV1ConfigMenubarGet**](V1Api.md#apiv1configmenubarget) | **GET** /api/v1/config/menubar | s.handleGetMenubarConfig
[**apiV1ConfigModelsPost**](V1Api.md#apiv1configmodelspost) | **POST** /api/v1/config/models | s.handleSaveModelsConfig
[**apiV1ConfigNormalizePost**](V1Api.md#apiv1confignormalizepost) | **POST** /api/v1/config/normalize | s.handleNormalizeConfig
[**apiV1DaemonRestartPost**](V1Api.md#apiv1daemonrestartpost) | **POST** /api/v1/daemon/restart | s.handleDaemonRestart
[**apiV1DaemonStartPost**](V1Api.md#apiv1daemonstartpost) | **POST** /api/v1/daemon/start | s.handleDaemonStart
[**apiV1DaemonStatusGet**](V1Api.md#apiv1daemonstatusget) | **GET** /api/v1/daemon/status | s.handleDaemonStatus
[**apiV1DaemonStopPost**](V1Api.md#apiv1daemonstoppost) | **POST** /api/v1/daemon/stop | s.handleDaemonStop
[**apiV1HealthGet**](V1Api.md#apiv1healthget) | **GET** /api/v1/health | s.handleHealth
[**apiV1MemoryExportPost**](V1Api.md#apiv1memoryexportpost) | **POST** /api/v1/memory/export | s.handleMemoryExport
[**apiV1MemoryQueryPost**](V1Api.md#apiv1memoryquerypost) | **POST** /api/v1/memory/query | s.handleMemoryQuery
[**apiV1MemoryRecentGet**](V1Api.md#apiv1memoryrecentget) | **GET** /api/v1/memory/recent | s.handleMemoryRecent
[**apiV1MemoryVectorIdDelete**](V1Api.md#apiv1memoryvectoriddelete) | **DELETE** /api/v1/memory/vector/{id} | s.handleMemoryVectorDelete
[**apiV1MemoryVectorSearchPost**](V1Api.md#apiv1memoryvectorsearchpost) | **POST** /api/v1/memory/vector/search | s.handleMemoryVectorSearch
[**apiV1MemoryVectorStatsGet**](V1Api.md#apiv1memoryvectorstatsget) | **GET** /api/v1/memory/vector/stats | s.handleMemoryVectorStats
[**apiV1MemoryVectorStorePost**](V1Api.md#apiv1memoryvectorstorepost) | **POST** /api/v1/memory/vector/store | s.handleMemoryVectorStore
[**apiV1MetricsFirewallGet**](V1Api.md#apiv1metricsfirewallget) | **GET** /api/v1/metrics/firewall | s.handleFirewallStats
[**apiV1MetricsHistoricalGet**](V1Api.md#apiv1metricshistoricalget) | **GET** /api/v1/metrics/historical | s.handleHistoricalMetrics
[**apiV1MetricsLiveGet**](V1Api.md#apiv1metricsliveget) | **GET** /api/v1/metrics/live | s.handleLiveMetrics
[**apiV1MetricsRateLimitsGet**](V1Api.md#apiv1metricsratelimitsget) | **GET** /api/v1/metrics/rate-limits | s.handleRateLimitSummary
[**apiV1MetricsStreamGet**](V1Api.md#apiv1metricsstreamget) | **GET** /api/v1/metrics/stream | s.handleMetricsStream
[**apiV1ModelsCredentialsProviderDelete**](V1Api.md#apiv1modelscredentialsproviderdelete) | **DELETE** /api/v1/models/credentials/{provider} | s.handleModelsDeleteCredential
[**apiV1ModelsDefaultPost**](V1Api.md#apiv1modelsdefaultpost) | **POST** /api/v1/models/default | s.handleModelsSetDefault
[**apiV1ModelsGet**](V1Api.md#apiv1modelsget) | **GET** /api/v1/models | s.handleModelsList
[**apiV1ModelsProviderModelDelete**](V1Api.md#apiv1modelsprovidermodeldelete) | **DELETE** /api/v1/models/{provider}/{model} | s.handleModelsRemove
[**apiV1ModelsProvidersGet**](V1Api.md#apiv1modelsprovidersget) | **GET** /api/v1/models/providers | s.handleModelsProviders
[**apiV1PlansGet**](V1Api.md#apiv1plansget) | **GET** /api/v1/plans | s.handlePlanList
[**apiV1PlansIdApprovePost**](V1Api.md#apiv1plansidapprovepost) | **POST** /api/v1/plans/{id}/approve | s.handlePlanApprove
[**apiV1PlansIdConfirmPost**](V1Api.md#apiv1plansidconfirmpost) | **POST** /api/v1/plans/{id}/confirm | s.handlePlanConfirm
[**apiV1PlansIdGet**](V1Api.md#apiv1plansidget) | **GET** /api/v1/plans/{id} | s.handlePlanGet
[**apiV1PlansIdRejectPost**](V1Api.md#apiv1plansidrejectpost) | **POST** /api/v1/plans/{id}/reject | s.handlePlanReject
[**apiV1PlansIdRevisePost**](V1Api.md#apiv1plansidrevisepost) | **POST** /api/v1/plans/{id}/revise | s.handlePlanRevise
[**apiV1ProjectsDetectPost**](V1Api.md#apiv1projectsdetectpost) | **POST** /api/v1/projects/detect | s.handleProjectDetect
[**apiV1ProjectsGet**](V1Api.md#apiv1projectsget) | **GET** /api/v1/projects | s.handleProjectList
[**apiV1ProjectsIdBranchesGet**](V1Api.md#apiv1projectsidbranchesget) | **GET** /api/v1/projects/{id}/branches | s.handleProjectBranches
[**apiV1ProjectsIdCheckoutPost**](V1Api.md#apiv1projectsidcheckoutpost) | **POST** /api/v1/projects/{id}/checkout | s.handleProjectCheckout
[**apiV1ProjectsIdGet**](V1Api.md#apiv1projectsidget) | **GET** /api/v1/projects/{id} | s.handleProjectGet
[**apiV1ProjectsIdStatusGet**](V1Api.md#apiv1projectsidstatusget) | **GET** /api/v1/projects/{id}/status | s.handleProjectStatus
[**apiV1ProjectsIdSyncPost**](V1Api.md#apiv1projectsidsyncpost) | **POST** /api/v1/projects/{id}/sync | s.handleProjectSync
[**apiV1QueueFollowupPost**](V1Api.md#apiv1queuefollowuppost) | **POST** /api/v1/queue/followup | s.handleQueueFollowUpRoute
[**apiV1QueueJobsGet**](V1Api.md#apiv1queuejobsget) | **GET** /api/v1/queue/jobs | s.handleQueueList
[**apiV1QueueJobsIdClaimPost**](V1Api.md#apiv1queuejobsidclaimpost) | **POST** /api/v1/queue/jobs/{id}/claim | s.handleQueueClaim
[**apiV1QueueJobsIdCompletePost**](V1Api.md#apiv1queuejobsidcompletepost) | **POST** /api/v1/queue/jobs/{id}/complete | s.handleQueueComplete
[**apiV1QueueJobsIdFailPost**](V1Api.md#apiv1queuejobsidfailpost) | **POST** /api/v1/queue/jobs/{id}/fail | s.handleQueueFail
[**apiV1QueueJobsIdGet**](V1Api.md#apiv1queuejobsidget) | **GET** /api/v1/queue/jobs/{id} | s.handleQueueGet
[**apiV1QueueJobsIdRetryPost**](V1Api.md#apiv1queuejobsidretrypost) | **POST** /api/v1/queue/jobs/{id}/retry | s.handleQueueRetry
[**apiV1QueueStatsGet**](V1Api.md#apiv1queuestatsget) | **GET** /api/v1/queue/stats | s.handleQueueStats
[**apiV1QueueStatusIdGet**](V1Api.md#apiv1queuestatusidget) | **GET** /api/v1/queue/status/{id} | s.handleQueueStatusRoute
[**apiV1QueueSteerPost**](V1Api.md#apiv1queuesteerpost) | **POST** /api/v1/queue/steer | s.handleQueueSteerRoute
[**apiV1RuntimeRestartProviderPost**](V1Api.md#apiv1runtimerestartproviderpost) | **POST** /api/v1/runtime/restart/{provider} | s.handleRuntimeRestart
[**apiV1RuntimeStartProviderPost**](V1Api.md#apiv1runtimestartproviderpost) | **POST** /api/v1/runtime/start/{provider} | s.handleRuntimeStart
[**apiV1RuntimeStatusGet**](V1Api.md#apiv1runtimestatusget) | **GET** /api/v1/runtime/status | s.handleRuntimeStatus
[**apiV1RuntimeStatusProviderGet**](V1Api.md#apiv1runtimestatusproviderget) | **GET** /api/v1/runtime/status/{provider} | s.handleRuntimeStatusProvider
[**apiV1RuntimeStopProviderPost**](V1Api.md#apiv1runtimestopproviderpost) | **POST** /api/v1/runtime/stop/{provider} | s.handleRuntimeStop
[**apiV1SchedulerJobsGet**](V1Api.md#apiv1schedulerjobsget) | **GET** /api/v1/scheduler/jobs | s.handleSchedulerListJobs
[**apiV1SchedulerJobsIdDelete**](V1Api.md#apiv1schedulerjobsiddelete) | **DELETE** /api/v1/scheduler/jobs/{id} | s.handleSchedulerRemoveJob
[**apiV1SchedulerJobsIdEnablePost**](V1Api.md#apiv1schedulerjobsidenablepost) | **POST** /api/v1/scheduler/jobs/{id}/enable | s.handleSchedulerEnableJob
[**apiV1SchedulerJobsIdPausePost**](V1Api.md#apiv1schedulerjobsidpausepost) | **POST** /api/v1/scheduler/jobs/{id}/pause | s.handleSchedulerPauseJob
[**apiV1SchedulerJobsIdResumePost**](V1Api.md#apiv1schedulerjobsidresumepost) | **POST** /api/v1/scheduler/jobs/{id}/resume | s.handleSchedulerResumeJob
[**apiV1SearchPost**](V1Api.md#apiv1searchpost) | **POST** /api/v1/search | s.handleSearch
[**apiV1SecurityCheckPost**](V1Api.md#apiv1securitycheckpost) | **POST** /api/v1/security/check | s.handleSecurityCheck
[**apiV1SelfimproveAnalyzePost**](V1Api.md#apiv1selfimproveanalyzepost) | **POST** /api/v1/selfimprove/analyze | s.handleSelfImproveAnalyze
[**apiV1SelfimproveApplyPost**](V1Api.md#apiv1selfimproveapplypost) | **POST** /api/v1/selfimprove/apply | s.handleSelfImproveApply
[**apiV1SelfimproveGeneratePost**](V1Api.md#apiv1selfimprovegeneratepost) | **POST** /api/v1/selfimprove/generate | s.handleSelfImproveGenerate
[**apiV1SelfimproveRejectPost**](V1Api.md#apiv1selfimproverejectpost) | **POST** /api/v1/selfimprove/reject | s.handleSelfImproveReject
[**apiV1SelfimproveStatusGet**](V1Api.md#apiv1selfimprovestatusget) | **GET** /api/v1/selfimprove/status | s.handleSelfImproveStatus
[**apiV1SelfimproveTriggerPost**](V1Api.md#apiv1selfimprovetriggerpost) | **POST** /api/v1/selfimprove/trigger | s.handleSelfImproveTrigger
[**apiV1SelfimproveValidatePost**](V1Api.md#apiv1selfimprovevalidatepost) | **POST** /api/v1/selfimprove/validate | s.handleSelfImproveValidate
[**apiV1SessionsGet**](V1Api.md#apiv1sessionsget) | **GET** /api/v1/sessions | s.handleSessionList
[**apiV1SessionsIdAttachPost**](V1Api.md#apiv1sessionsidattachpost) | **POST** /api/v1/sessions/{id}/attach | s.handleSessionAttach
[**apiV1SessionsIdBranchPost**](V1Api.md#apiv1sessionsidbranchpost) | **POST** /api/v1/sessions/{id}/branch | s.handleSessionBranch
[**apiV1SessionsIdBranchesGet**](V1Api.md#apiv1sessionsidbranchesget) | **GET** /api/v1/sessions/{id}/branches | s.handleSessionBranches
[**apiV1SessionsIdCompactPost**](V1Api.md#apiv1sessionsidcompactpost) | **POST** /api/v1/sessions/{id}/compact | s.handleSessionCompact
[**apiV1SessionsIdDelete**](V1Api.md#apiv1sessionsiddelete) | **DELETE** /api/v1/sessions/{id} | s.handleSessionDelete
[**apiV1SessionsIdDetachPost**](V1Api.md#apiv1sessionsiddetachpost) | **POST** /api/v1/sessions/{id}/detach | s.handleSessionDetach
[**apiV1SessionsIdForkPost**](V1Api.md#apiv1sessionsidforkpost) | **POST** /api/v1/sessions/{id}/fork | s.handleSessionFork
[**apiV1SessionsIdMessagesGet**](V1Api.md#apiv1sessionsidmessagesget) | **GET** /api/v1/sessions/{id}/messages | s.handleSessionMessages
[**apiV1SessionsIdPlansGet**](V1Api.md#apiv1sessionsidplansget) | **GET** /api/v1/sessions/{id}/plans | s.handleSessionPlans
[**apiV1SessionsIdResumePost**](V1Api.md#apiv1sessionsidresumepost) | **POST** /api/v1/sessions/{id}/resume | s.handleSessionResume
[**apiV1SessionsIdTreeGet**](V1Api.md#apiv1sessionsidtreeget) | **GET** /api/v1/sessions/{id}/tree | s.handleSessionTree
[**apiV1SkillsGet**](V1Api.md#apiv1skillsget) | **GET** /api/v1/skills | s.handleSkillsList
[**apiV1SkillsSlugExecutePost**](V1Api.md#apiv1skillsslugexecutepost) | **POST** /api/v1/skills/{slug}/execute | s.handleSkillsExecute
[**apiV1SkillsSlugGet**](V1Api.md#apiv1skillsslugget) | **GET** /api/v1/skills/{slug} | s.handleSkillsGet
[**apiV1SkillsSlugUiGet**](V1Api.md#apiv1skillssluguiget) | **GET** /api/v1/skills/{slug}/ui | s.handleSkillUI
[**apiV1TasksGet**](V1Api.md#apiv1tasksget) | **GET** /api/v1/tasks | s.handleTaskList
[**apiV1TasksIdCancelPost**](V1Api.md#apiv1tasksidcancelpost) | **POST** /api/v1/tasks/{id}/cancel | s.handleTaskCancel
[**apiV1TasksIdDelete**](V1Api.md#apiv1tasksiddelete) | **DELETE** /api/v1/tasks/{id} | s.handleTaskDelete
[**apiV1TasksIdStepsGet**](V1Api.md#apiv1tasksidstepsget) | **GET** /api/v1/tasks/{id}/steps | s.handleTaskSteps
[**apiV1TerminalClearPost**](V1Api.md#apiv1terminalclearpost) | **POST** /api/v1/terminal/clear | s.handleTerminalClear
[**apiV1TerminalExecPost**](V1Api.md#apiv1terminalexecpost) | **POST** /api/v1/terminal/exec | s.handleTerminalExec
[**apiV1TerminalHistoryGet**](V1Api.md#apiv1terminalhistoryget) | **GET** /api/v1/terminal/history | s.handleTerminalHistory
[**apiV1TerminalSessionsGet**](V1Api.md#apiv1terminalsessionsget) | **GET** /api/v1/terminal/sessions | s.handleTerminalSessions
[**apiV1WorkersIdDelete**](V1Api.md#apiv1workersiddelete) | **DELETE** /api/v1/workers/{id} | s.handleWorkerRemove
[**apiV1WorkersPost**](V1Api.md#apiv1workerspost) | **POST** /api/v1/workers | s.handleWorkerAdd
[**apiV1WorkersScalePost**](V1Api.md#apiv1workersscalepost) | **POST** /api/v1/workers/scale | s.handleWorkerScale
[**apiV1WorkersStatsGet**](V1Api.md#apiv1workersstatsget) | **GET** /api/v1/workers/stats | s.handleWorkerStats


# **apiV1BusPublishPost**
> apiV1BusPublishPost()

s.handleBusPublish

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1BusPublishPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1BusPublishPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1BusStatsGet**
> apiV1BusStatsGet()

s.handleBusStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1BusStatsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1BusStatsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CacheClearPost**
> apiV1CacheClearPost()

s.handleCacheClear

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CacheClearPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1CacheClearPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CacheInspectGet**
> apiV1CacheInspectGet()

s.handleCacheInspect

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CacheInspectGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CacheInspectGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CacheInvalidatePost**
> apiV1CacheInvalidatePost()

s.handleCacheInvalidate

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CacheInvalidatePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1CacheInvalidatePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CacheStatsGet**
> apiV1CacheStatsGet()

s.handleCacheStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CacheStatsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CacheStatsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CalendarEventsGet**
> apiV1CalendarEventsGet()

s.handleCalendarList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CalendarEventsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CalendarEventsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CalendarEventsIdGet**
> apiV1CalendarEventsIdGet()

s.handleCalendarGet

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CalendarEventsIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CalendarEventsIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CalendarQuickaddPost**
> apiV1CalendarQuickaddPost()

s.handleCalendarQuickAdd

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CalendarQuickaddPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1CalendarQuickaddPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CalendarTodayGet**
> apiV1CalendarTodayGet()

s.handleCalendarToday

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CalendarTodayGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CalendarTodayGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1CalendarUpcomingGet**
> apiV1CalendarUpcomingGet()

s.handleCalendarUpcoming

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1CalendarUpcomingGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1CalendarUpcomingGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ChatPost**
> apiV1ChatPost()

s.handleChat

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ChatPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ChatPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ChatQueueIdGet**
> apiV1ChatQueueIdGet()

s.handleChatQueueStatus

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ChatQueueIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ChatQueueIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ChatStreamGet**
> apiV1ChatStreamGet()

s.handleChatStream

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ChatStreamGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ChatStreamGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ChatWithAgentPost**
> apiV1ChatWithAgentPost()

s.handleChatWithAgent

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ChatWithAgentPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ChatWithAgentPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigAgentsGet**
> apiV1ConfigAgentsGet()

s.handleListAgents

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigAgentsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigAgentsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigAgentsIdPost**
> apiV1ConfigAgentsIdPost()

s.handleSaveAgent

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigAgentsIdPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigAgentsIdPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigClientPost**
> apiV1ConfigClientPost()

s.handleSaveClientConfig

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigClientPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigClientPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigMenubarGet**
> apiV1ConfigMenubarGet()

s.handleGetMenubarConfig

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigMenubarGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigMenubarGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigModelsPost**
> apiV1ConfigModelsPost()

s.handleSaveModelsConfig

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigModelsPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigModelsPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ConfigNormalizePost**
> apiV1ConfigNormalizePost()

s.handleNormalizeConfig

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ConfigNormalizePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ConfigNormalizePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1DaemonRestartPost**
> apiV1DaemonRestartPost()

s.handleDaemonRestart

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1DaemonRestartPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1DaemonRestartPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1DaemonStartPost**
> apiV1DaemonStartPost()

s.handleDaemonStart

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1DaemonStartPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1DaemonStartPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1DaemonStatusGet**
> apiV1DaemonStatusGet()

s.handleDaemonStatus

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1DaemonStatusGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1DaemonStatusGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1DaemonStopPost**
> apiV1DaemonStopPost()

s.handleDaemonStop

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1DaemonStopPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1DaemonStopPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1HealthGet**
> apiV1HealthGet()

s.handleHealth

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1HealthGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1HealthGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryExportPost**
> apiV1MemoryExportPost()

s.handleMemoryExport

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryExportPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryExportPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryQueryPost**
> apiV1MemoryQueryPost()

s.handleMemoryQuery

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryQueryPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryQueryPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryRecentGet**
> apiV1MemoryRecentGet()

s.handleMemoryRecent

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryRecentGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryRecentGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryVectorIdDelete**
> apiV1MemoryVectorIdDelete()

s.handleMemoryVectorDelete

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryVectorIdDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryVectorIdDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryVectorSearchPost**
> apiV1MemoryVectorSearchPost()

s.handleMemoryVectorSearch

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryVectorSearchPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryVectorSearchPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryVectorStatsGet**
> apiV1MemoryVectorStatsGet()

s.handleMemoryVectorStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryVectorStatsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryVectorStatsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MemoryVectorStorePost**
> apiV1MemoryVectorStorePost()

s.handleMemoryVectorStore

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MemoryVectorStorePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1MemoryVectorStorePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MetricsFirewallGet**
> apiV1MetricsFirewallGet()

s.handleFirewallStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MetricsFirewallGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MetricsFirewallGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MetricsHistoricalGet**
> apiV1MetricsHistoricalGet()

s.handleHistoricalMetrics

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MetricsHistoricalGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MetricsHistoricalGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MetricsLiveGet**
> apiV1MetricsLiveGet()

s.handleLiveMetrics

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MetricsLiveGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MetricsLiveGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MetricsRateLimitsGet**
> apiV1MetricsRateLimitsGet()

s.handleRateLimitSummary

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MetricsRateLimitsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MetricsRateLimitsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1MetricsStreamGet**
> apiV1MetricsStreamGet()

s.handleMetricsStream

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1MetricsStreamGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1MetricsStreamGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ModelsCredentialsProviderDelete**
> apiV1ModelsCredentialsProviderDelete()

s.handleModelsDeleteCredential

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ModelsCredentialsProviderDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1ModelsCredentialsProviderDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ModelsDefaultPost**
> apiV1ModelsDefaultPost()

s.handleModelsSetDefault

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ModelsDefaultPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ModelsDefaultPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ModelsGet**
> apiV1ModelsGet()

s.handleModelsList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ModelsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ModelsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ModelsProviderModelDelete**
> apiV1ModelsProviderModelDelete()

s.handleModelsRemove

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ModelsProviderModelDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1ModelsProviderModelDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ModelsProvidersGet**
> apiV1ModelsProvidersGet()

s.handleModelsProviders

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ModelsProvidersGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ModelsProvidersGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansGet**
> apiV1PlansGet()

s.handlePlanList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansIdApprovePost**
> apiV1PlansIdApprovePost()

s.handlePlanApprove

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansIdApprovePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansIdApprovePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansIdConfirmPost**
> apiV1PlansIdConfirmPost()

s.handlePlanConfirm

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansIdConfirmPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansIdConfirmPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansIdGet**
> apiV1PlansIdGet()

s.handlePlanGet

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansIdRejectPost**
> apiV1PlansIdRejectPost()

s.handlePlanReject

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansIdRejectPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansIdRejectPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PlansIdRevisePost**
> apiV1PlansIdRevisePost()

s.handlePlanRevise

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1PlansIdRevisePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1PlansIdRevisePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsDetectPost**
> apiV1ProjectsDetectPost()

s.handleProjectDetect

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsDetectPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsDetectPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsGet**
> apiV1ProjectsGet()

s.handleProjectList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsIdBranchesGet**
> apiV1ProjectsIdBranchesGet()

s.handleProjectBranches

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsIdBranchesGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsIdBranchesGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsIdCheckoutPost**
> apiV1ProjectsIdCheckoutPost()

s.handleProjectCheckout

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsIdCheckoutPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsIdCheckoutPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsIdGet**
> apiV1ProjectsIdGet()

s.handleProjectGet

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsIdStatusGet**
> apiV1ProjectsIdStatusGet()

s.handleProjectStatus

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsIdStatusGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsIdStatusGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1ProjectsIdSyncPost**
> apiV1ProjectsIdSyncPost()

s.handleProjectSync

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1ProjectsIdSyncPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1ProjectsIdSyncPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueFollowupPost**
> apiV1QueueFollowupPost()

s.handleQueueFollowUpRoute

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueFollowupPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueFollowupPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsGet**
> apiV1QueueJobsGet()

s.handleQueueList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsIdClaimPost**
> apiV1QueueJobsIdClaimPost()

s.handleQueueClaim

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsIdClaimPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsIdClaimPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsIdCompletePost**
> apiV1QueueJobsIdCompletePost()

s.handleQueueComplete

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsIdCompletePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsIdCompletePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsIdFailPost**
> apiV1QueueJobsIdFailPost()

s.handleQueueFail

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsIdFailPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsIdFailPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsIdGet**
> apiV1QueueJobsIdGet()

s.handleQueueGet

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueJobsIdRetryPost**
> apiV1QueueJobsIdRetryPost()

s.handleQueueRetry

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueJobsIdRetryPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueJobsIdRetryPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueStatsGet**
> apiV1QueueStatsGet()

s.handleQueueStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueStatsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueStatsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueStatusIdGet**
> apiV1QueueStatusIdGet()

s.handleQueueStatusRoute

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueStatusIdGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueStatusIdGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1QueueSteerPost**
> apiV1QueueSteerPost()

s.handleQueueSteerRoute

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1QueueSteerPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1QueueSteerPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1RuntimeRestartProviderPost**
> apiV1RuntimeRestartProviderPost()

s.handleRuntimeRestart

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1RuntimeRestartProviderPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1RuntimeRestartProviderPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1RuntimeStartProviderPost**
> apiV1RuntimeStartProviderPost()

s.handleRuntimeStart

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1RuntimeStartProviderPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1RuntimeStartProviderPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1RuntimeStatusGet**
> apiV1RuntimeStatusGet()

s.handleRuntimeStatus

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1RuntimeStatusGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1RuntimeStatusGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1RuntimeStatusProviderGet**
> apiV1RuntimeStatusProviderGet()

s.handleRuntimeStatusProvider

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1RuntimeStatusProviderGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1RuntimeStatusProviderGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1RuntimeStopProviderPost**
> apiV1RuntimeStopProviderPost()

s.handleRuntimeStop

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1RuntimeStopProviderPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1RuntimeStopProviderPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SchedulerJobsGet**
> apiV1SchedulerJobsGet()

s.handleSchedulerListJobs

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SchedulerJobsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SchedulerJobsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SchedulerJobsIdDelete**
> apiV1SchedulerJobsIdDelete()

s.handleSchedulerRemoveJob

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SchedulerJobsIdDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1SchedulerJobsIdDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SchedulerJobsIdEnablePost**
> apiV1SchedulerJobsIdEnablePost()

s.handleSchedulerEnableJob

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SchedulerJobsIdEnablePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SchedulerJobsIdEnablePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SchedulerJobsIdPausePost**
> apiV1SchedulerJobsIdPausePost()

s.handleSchedulerPauseJob

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SchedulerJobsIdPausePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SchedulerJobsIdPausePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SchedulerJobsIdResumePost**
> apiV1SchedulerJobsIdResumePost()

s.handleSchedulerResumeJob

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SchedulerJobsIdResumePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SchedulerJobsIdResumePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SearchPost**
> apiV1SearchPost()

s.handleSearch

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SearchPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SearchPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SecurityCheckPost**
> apiV1SecurityCheckPost()

s.handleSecurityCheck

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SecurityCheckPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SecurityCheckPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveAnalyzePost**
> apiV1SelfimproveAnalyzePost()

s.handleSelfImproveAnalyze

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveAnalyzePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveAnalyzePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveApplyPost**
> apiV1SelfimproveApplyPost()

s.handleSelfImproveApply

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveApplyPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveApplyPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveGeneratePost**
> apiV1SelfimproveGeneratePost()

s.handleSelfImproveGenerate

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveGeneratePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveGeneratePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveRejectPost**
> apiV1SelfimproveRejectPost()

s.handleSelfImproveReject

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveRejectPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveRejectPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveStatusGet**
> apiV1SelfimproveStatusGet()

s.handleSelfImproveStatus

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveStatusGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveStatusGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveTriggerPost**
> apiV1SelfimproveTriggerPost()

s.handleSelfImproveTrigger

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveTriggerPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveTriggerPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SelfimproveValidatePost**
> apiV1SelfimproveValidatePost()

s.handleSelfImproveValidate

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SelfimproveValidatePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SelfimproveValidatePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsGet**
> apiV1SessionsGet()

s.handleSessionList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdAttachPost**
> apiV1SessionsIdAttachPost()

s.handleSessionAttach

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdAttachPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdAttachPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdBranchPost**
> apiV1SessionsIdBranchPost()

s.handleSessionBranch

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdBranchPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdBranchPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdBranchesGet**
> apiV1SessionsIdBranchesGet()

s.handleSessionBranches

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdBranchesGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdBranchesGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdCompactPost**
> apiV1SessionsIdCompactPost()

s.handleSessionCompact

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdCompactPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdCompactPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdDelete**
> apiV1SessionsIdDelete()

s.handleSessionDelete

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdDetachPost**
> apiV1SessionsIdDetachPost()

s.handleSessionDetach

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdDetachPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdDetachPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdForkPost**
> apiV1SessionsIdForkPost()

s.handleSessionFork

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdForkPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdForkPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdMessagesGet**
> apiV1SessionsIdMessagesGet()

s.handleSessionMessages

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdMessagesGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdMessagesGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdPlansGet**
> apiV1SessionsIdPlansGet()

s.handleSessionPlans

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdPlansGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdPlansGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdResumePost**
> apiV1SessionsIdResumePost()

s.handleSessionResume

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdResumePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdResumePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SessionsIdTreeGet**
> apiV1SessionsIdTreeGet()

s.handleSessionTree

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SessionsIdTreeGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SessionsIdTreeGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SkillsGet**
> apiV1SkillsGet()

s.handleSkillsList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SkillsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SkillsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SkillsSlugExecutePost**
> apiV1SkillsSlugExecutePost()

s.handleSkillsExecute

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SkillsSlugExecutePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1SkillsSlugExecutePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SkillsSlugGet**
> apiV1SkillsSlugGet()

s.handleSkillsGet

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SkillsSlugGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SkillsSlugGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1SkillsSlugUiGet**
> apiV1SkillsSlugUiGet()

s.handleSkillUI

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1SkillsSlugUiGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1SkillsSlugUiGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TasksGet**
> apiV1TasksGet()

s.handleTaskList

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TasksGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1TasksGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TasksIdCancelPost**
> apiV1TasksIdCancelPost()

s.handleTaskCancel

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TasksIdCancelPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1TasksIdCancelPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TasksIdDelete**
> apiV1TasksIdDelete()

s.handleTaskDelete

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TasksIdDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1TasksIdDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TasksIdStepsGet**
> apiV1TasksIdStepsGet()

s.handleTaskSteps

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TasksIdStepsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1TasksIdStepsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TerminalClearPost**
> apiV1TerminalClearPost()

s.handleTerminalClear

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TerminalClearPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1TerminalClearPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TerminalExecPost**
> apiV1TerminalExecPost()

s.handleTerminalExec

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TerminalExecPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1TerminalExecPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TerminalHistoryGet**
> apiV1TerminalHistoryGet()

s.handleTerminalHistory

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TerminalHistoryGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1TerminalHistoryGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1TerminalSessionsGet**
> apiV1TerminalSessionsGet()

s.handleTerminalSessions

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1TerminalSessionsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1TerminalSessionsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1WorkersIdDelete**
> apiV1WorkersIdDelete()

s.handleWorkerRemove

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1WorkersIdDelete();
} catch (e) {
    print('Exception when calling V1Api->apiV1WorkersIdDelete: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1WorkersPost**
> apiV1WorkersPost()

s.handleWorkerAdd

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1WorkersPost();
} catch (e) {
    print('Exception when calling V1Api->apiV1WorkersPost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1WorkersScalePost**
> apiV1WorkersScalePost()

s.handleWorkerScale

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1WorkersScalePost();
} catch (e) {
    print('Exception when calling V1Api->apiV1WorkersScalePost: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1WorkersStatsGet**
> apiV1WorkersStatsGet()

s.handleWorkerStats

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api_instance = V1Api();

try {
    api_instance.apiV1WorkersStatsGet();
} catch (e) {
    print('Exception when calling V1Api->apiV1WorkersStatsGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

