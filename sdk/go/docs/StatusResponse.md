# StatusResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Enabled** | **bool** |  | 
**LastCycleomitempty** | Pointer to **string** |  | [optional] 
**SkillsLearned** | **int32** |  | 
**PendingTasks** | **int32** |  | 

## Methods

### NewStatusResponse

`func NewStatusResponse(enabled bool, skillsLearned int32, pendingTasks int32, ) *StatusResponse`

NewStatusResponse instantiates a new StatusResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewStatusResponseWithDefaults

`func NewStatusResponseWithDefaults() *StatusResponse`

NewStatusResponseWithDefaults instantiates a new StatusResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetEnabled

`func (o *StatusResponse) GetEnabled() bool`

GetEnabled returns the Enabled field if non-nil, zero value otherwise.

### GetEnabledOk

`func (o *StatusResponse) GetEnabledOk() (*bool, bool)`

GetEnabledOk returns a tuple with the Enabled field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnabled

`func (o *StatusResponse) SetEnabled(v bool)`

SetEnabled sets Enabled field to given value.


### GetLastCycleomitempty

`func (o *StatusResponse) GetLastCycleomitempty() string`

GetLastCycleomitempty returns the LastCycleomitempty field if non-nil, zero value otherwise.

### GetLastCycleomitemptyOk

`func (o *StatusResponse) GetLastCycleomitemptyOk() (*string, bool)`

GetLastCycleomitemptyOk returns a tuple with the LastCycleomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLastCycleomitempty

`func (o *StatusResponse) SetLastCycleomitempty(v string)`

SetLastCycleomitempty sets LastCycleomitempty field to given value.

### HasLastCycleomitempty

`func (o *StatusResponse) HasLastCycleomitempty() bool`

HasLastCycleomitempty returns a boolean if a field has been set.

### GetSkillsLearned

`func (o *StatusResponse) GetSkillsLearned() int32`

GetSkillsLearned returns the SkillsLearned field if non-nil, zero value otherwise.

### GetSkillsLearnedOk

`func (o *StatusResponse) GetSkillsLearnedOk() (*int32, bool)`

GetSkillsLearnedOk returns a tuple with the SkillsLearned field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSkillsLearned

`func (o *StatusResponse) SetSkillsLearned(v int32)`

SetSkillsLearned sets SkillsLearned field to given value.


### GetPendingTasks

`func (o *StatusResponse) GetPendingTasks() int32`

GetPendingTasks returns the PendingTasks field if non-nil, zero value otherwise.

### GetPendingTasksOk

`func (o *StatusResponse) GetPendingTasksOk() (*int32, bool)`

GetPendingTasksOk returns a tuple with the PendingTasks field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPendingTasks

`func (o *StatusResponse) SetPendingTasks(v int32)`

SetPendingTasks sets PendingTasks field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


