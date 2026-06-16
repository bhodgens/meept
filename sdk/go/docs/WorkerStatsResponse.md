# WorkerStatsResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**TotalWorkers** | **int32** |  | 
**IdleWorkers** | **int32** |  | 
**BusyWorkers** | **int32** |  | 
**ErrorWorkers** | **int32** |  | 
**WorkerStats** | **[]string** |  | 

## Methods

### NewWorkerStatsResponse

`func NewWorkerStatsResponse(totalWorkers int32, idleWorkers int32, busyWorkers int32, errorWorkers int32, workerStats []string, ) *WorkerStatsResponse`

NewWorkerStatsResponse instantiates a new WorkerStatsResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewWorkerStatsResponseWithDefaults

`func NewWorkerStatsResponseWithDefaults() *WorkerStatsResponse`

NewWorkerStatsResponseWithDefaults instantiates a new WorkerStatsResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetTotalWorkers

`func (o *WorkerStatsResponse) GetTotalWorkers() int32`

GetTotalWorkers returns the TotalWorkers field if non-nil, zero value otherwise.

### GetTotalWorkersOk

`func (o *WorkerStatsResponse) GetTotalWorkersOk() (*int32, bool)`

GetTotalWorkersOk returns a tuple with the TotalWorkers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTotalWorkers

`func (o *WorkerStatsResponse) SetTotalWorkers(v int32)`

SetTotalWorkers sets TotalWorkers field to given value.


### GetIdleWorkers

`func (o *WorkerStatsResponse) GetIdleWorkers() int32`

GetIdleWorkers returns the IdleWorkers field if non-nil, zero value otherwise.

### GetIdleWorkersOk

`func (o *WorkerStatsResponse) GetIdleWorkersOk() (*int32, bool)`

GetIdleWorkersOk returns a tuple with the IdleWorkers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetIdleWorkers

`func (o *WorkerStatsResponse) SetIdleWorkers(v int32)`

SetIdleWorkers sets IdleWorkers field to given value.


### GetBusyWorkers

`func (o *WorkerStatsResponse) GetBusyWorkers() int32`

GetBusyWorkers returns the BusyWorkers field if non-nil, zero value otherwise.

### GetBusyWorkersOk

`func (o *WorkerStatsResponse) GetBusyWorkersOk() (*int32, bool)`

GetBusyWorkersOk returns a tuple with the BusyWorkers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBusyWorkers

`func (o *WorkerStatsResponse) SetBusyWorkers(v int32)`

SetBusyWorkers sets BusyWorkers field to given value.


### GetErrorWorkers

`func (o *WorkerStatsResponse) GetErrorWorkers() int32`

GetErrorWorkers returns the ErrorWorkers field if non-nil, zero value otherwise.

### GetErrorWorkersOk

`func (o *WorkerStatsResponse) GetErrorWorkersOk() (*int32, bool)`

GetErrorWorkersOk returns a tuple with the ErrorWorkers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErrorWorkers

`func (o *WorkerStatsResponse) SetErrorWorkers(v int32)`

SetErrorWorkers sets ErrorWorkers field to given value.


### GetWorkerStats

`func (o *WorkerStatsResponse) GetWorkerStats() []string`

GetWorkerStats returns the WorkerStats field if non-nil, zero value otherwise.

### GetWorkerStatsOk

`func (o *WorkerStatsResponse) GetWorkerStatsOk() (*[]string, bool)`

GetWorkerStatsOk returns a tuple with the WorkerStats field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkerStats

`func (o *WorkerStatsResponse) SetWorkerStats(v []string)`

SetWorkerStats sets WorkerStats field to given value.


### SetWorkerStatsNil

`func (o *WorkerStatsResponse) SetWorkerStatsNil(b bool)`

 SetWorkerStatsNil sets the value for WorkerStats to be an explicit nil

### UnsetWorkerStats
`func (o *WorkerStatsResponse) UnsetWorkerStats()`

UnsetWorkerStats ensures that no value is present for WorkerStats, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


