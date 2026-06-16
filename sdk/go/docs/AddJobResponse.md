# AddJobResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Name** | **string** |  | 
**Schedule** | **string** |  | 
**Enabled** | **bool** |  | 
**LastRunomitempty** | Pointer to **NullableString** |  | [optional] 
**NextRunomitempty** | Pointer to **NullableString** |  | [optional] 
**LastErroromitempty** | Pointer to **string** |  | [optional] 
**RunCount** | **int32** |  | 
**IsRunning** | **bool** |  | 

## Methods

### NewAddJobResponse

`func NewAddJobResponse(id string, name string, schedule string, enabled bool, runCount int32, isRunning bool, ) *AddJobResponse`

NewAddJobResponse instantiates a new AddJobResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewAddJobResponseWithDefaults

`func NewAddJobResponseWithDefaults() *AddJobResponse`

NewAddJobResponseWithDefaults instantiates a new AddJobResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *AddJobResponse) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *AddJobResponse) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *AddJobResponse) SetId(v string)`

SetId sets Id field to given value.


### GetName

`func (o *AddJobResponse) GetName() string`

GetName returns the Name field if non-nil, zero value otherwise.

### GetNameOk

`func (o *AddJobResponse) GetNameOk() (*string, bool)`

GetNameOk returns a tuple with the Name field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetName

`func (o *AddJobResponse) SetName(v string)`

SetName sets Name field to given value.


### GetSchedule

`func (o *AddJobResponse) GetSchedule() string`

GetSchedule returns the Schedule field if non-nil, zero value otherwise.

### GetScheduleOk

`func (o *AddJobResponse) GetScheduleOk() (*string, bool)`

GetScheduleOk returns a tuple with the Schedule field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSchedule

`func (o *AddJobResponse) SetSchedule(v string)`

SetSchedule sets Schedule field to given value.


### GetEnabled

`func (o *AddJobResponse) GetEnabled() bool`

GetEnabled returns the Enabled field if non-nil, zero value otherwise.

### GetEnabledOk

`func (o *AddJobResponse) GetEnabledOk() (*bool, bool)`

GetEnabledOk returns a tuple with the Enabled field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnabled

`func (o *AddJobResponse) SetEnabled(v bool)`

SetEnabled sets Enabled field to given value.


### GetLastRunomitempty

`func (o *AddJobResponse) GetLastRunomitempty() string`

GetLastRunomitempty returns the LastRunomitempty field if non-nil, zero value otherwise.

### GetLastRunomitemptyOk

`func (o *AddJobResponse) GetLastRunomitemptyOk() (*string, bool)`

GetLastRunomitemptyOk returns a tuple with the LastRunomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLastRunomitempty

`func (o *AddJobResponse) SetLastRunomitempty(v string)`

SetLastRunomitempty sets LastRunomitempty field to given value.

### HasLastRunomitempty

`func (o *AddJobResponse) HasLastRunomitempty() bool`

HasLastRunomitempty returns a boolean if a field has been set.

### SetLastRunomitemptyNil

`func (o *AddJobResponse) SetLastRunomitemptyNil(b bool)`

 SetLastRunomitemptyNil sets the value for LastRunomitempty to be an explicit nil

### UnsetLastRunomitempty
`func (o *AddJobResponse) UnsetLastRunomitempty()`

UnsetLastRunomitempty ensures that no value is present for LastRunomitempty, not even an explicit nil
### GetNextRunomitempty

`func (o *AddJobResponse) GetNextRunomitempty() string`

GetNextRunomitempty returns the NextRunomitempty field if non-nil, zero value otherwise.

### GetNextRunomitemptyOk

`func (o *AddJobResponse) GetNextRunomitemptyOk() (*string, bool)`

GetNextRunomitemptyOk returns a tuple with the NextRunomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetNextRunomitempty

`func (o *AddJobResponse) SetNextRunomitempty(v string)`

SetNextRunomitempty sets NextRunomitempty field to given value.

### HasNextRunomitempty

`func (o *AddJobResponse) HasNextRunomitempty() bool`

HasNextRunomitempty returns a boolean if a field has been set.

### SetNextRunomitemptyNil

`func (o *AddJobResponse) SetNextRunomitemptyNil(b bool)`

 SetNextRunomitemptyNil sets the value for NextRunomitempty to be an explicit nil

### UnsetNextRunomitempty
`func (o *AddJobResponse) UnsetNextRunomitempty()`

UnsetNextRunomitempty ensures that no value is present for NextRunomitempty, not even an explicit nil
### GetLastErroromitempty

`func (o *AddJobResponse) GetLastErroromitempty() string`

GetLastErroromitempty returns the LastErroromitempty field if non-nil, zero value otherwise.

### GetLastErroromitemptyOk

`func (o *AddJobResponse) GetLastErroromitemptyOk() (*string, bool)`

GetLastErroromitemptyOk returns a tuple with the LastErroromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLastErroromitempty

`func (o *AddJobResponse) SetLastErroromitempty(v string)`

SetLastErroromitempty sets LastErroromitempty field to given value.

### HasLastErroromitempty

`func (o *AddJobResponse) HasLastErroromitempty() bool`

HasLastErroromitempty returns a boolean if a field has been set.

### GetRunCount

`func (o *AddJobResponse) GetRunCount() int32`

GetRunCount returns the RunCount field if non-nil, zero value otherwise.

### GetRunCountOk

`func (o *AddJobResponse) GetRunCountOk() (*int32, bool)`

GetRunCountOk returns a tuple with the RunCount field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRunCount

`func (o *AddJobResponse) SetRunCount(v int32)`

SetRunCount sets RunCount field to given value.


### GetIsRunning

`func (o *AddJobResponse) GetIsRunning() bool`

GetIsRunning returns the IsRunning field if non-nil, zero value otherwise.

### GetIsRunningOk

`func (o *AddJobResponse) GetIsRunningOk() (*bool, bool)`

GetIsRunningOk returns a tuple with the IsRunning field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetIsRunning

`func (o *AddJobResponse) SetIsRunning(v bool)`

SetIsRunning sets IsRunning field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


