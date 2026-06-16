# CommandHistory

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**Command** | **string** |  | 
**Outputomitempty** | Pointer to **string** |  | [optional] 
**Stderromitempty** | Pointer to **string** |  | [optional] 
**ExitCode** | **int32** |  | 
**Timestamp** | **string** |  | 
**WorkingDir** | **string** |  | 
**DurationMs** | **map[string]interface{}** |  | 
**RiskLevel** | **map[string]interface{}** |  | 
**Success** | **bool** |  | 

## Methods

### NewCommandHistory

`func NewCommandHistory(id string, command string, exitCode int32, timestamp string, workingDir string, durationMs map[string]interface{}, riskLevel map[string]interface{}, success bool, ) *CommandHistory`

NewCommandHistory instantiates a new CommandHistory object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCommandHistoryWithDefaults

`func NewCommandHistoryWithDefaults() *CommandHistory`

NewCommandHistoryWithDefaults instantiates a new CommandHistory object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *CommandHistory) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *CommandHistory) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *CommandHistory) SetId(v string)`

SetId sets Id field to given value.


### GetCommand

`func (o *CommandHistory) GetCommand() string`

GetCommand returns the Command field if non-nil, zero value otherwise.

### GetCommandOk

`func (o *CommandHistory) GetCommandOk() (*string, bool)`

GetCommandOk returns a tuple with the Command field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCommand

`func (o *CommandHistory) SetCommand(v string)`

SetCommand sets Command field to given value.


### GetOutputomitempty

`func (o *CommandHistory) GetOutputomitempty() string`

GetOutputomitempty returns the Outputomitempty field if non-nil, zero value otherwise.

### GetOutputomitemptyOk

`func (o *CommandHistory) GetOutputomitemptyOk() (*string, bool)`

GetOutputomitemptyOk returns a tuple with the Outputomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOutputomitempty

`func (o *CommandHistory) SetOutputomitempty(v string)`

SetOutputomitempty sets Outputomitempty field to given value.

### HasOutputomitempty

`func (o *CommandHistory) HasOutputomitempty() bool`

HasOutputomitempty returns a boolean if a field has been set.

### GetStderromitempty

`func (o *CommandHistory) GetStderromitempty() string`

GetStderromitempty returns the Stderromitempty field if non-nil, zero value otherwise.

### GetStderromitemptyOk

`func (o *CommandHistory) GetStderromitemptyOk() (*string, bool)`

GetStderromitemptyOk returns a tuple with the Stderromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStderromitempty

`func (o *CommandHistory) SetStderromitempty(v string)`

SetStderromitempty sets Stderromitempty field to given value.

### HasStderromitempty

`func (o *CommandHistory) HasStderromitempty() bool`

HasStderromitempty returns a boolean if a field has been set.

### GetExitCode

`func (o *CommandHistory) GetExitCode() int32`

GetExitCode returns the ExitCode field if non-nil, zero value otherwise.

### GetExitCodeOk

`func (o *CommandHistory) GetExitCodeOk() (*int32, bool)`

GetExitCodeOk returns a tuple with the ExitCode field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetExitCode

`func (o *CommandHistory) SetExitCode(v int32)`

SetExitCode sets ExitCode field to given value.


### GetTimestamp

`func (o *CommandHistory) GetTimestamp() string`

GetTimestamp returns the Timestamp field if non-nil, zero value otherwise.

### GetTimestampOk

`func (o *CommandHistory) GetTimestampOk() (*string, bool)`

GetTimestampOk returns a tuple with the Timestamp field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTimestamp

`func (o *CommandHistory) SetTimestamp(v string)`

SetTimestamp sets Timestamp field to given value.


### GetWorkingDir

`func (o *CommandHistory) GetWorkingDir() string`

GetWorkingDir returns the WorkingDir field if non-nil, zero value otherwise.

### GetWorkingDirOk

`func (o *CommandHistory) GetWorkingDirOk() (*string, bool)`

GetWorkingDirOk returns a tuple with the WorkingDir field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkingDir

`func (o *CommandHistory) SetWorkingDir(v string)`

SetWorkingDir sets WorkingDir field to given value.


### GetDurationMs

`func (o *CommandHistory) GetDurationMs() map[string]interface{}`

GetDurationMs returns the DurationMs field if non-nil, zero value otherwise.

### GetDurationMsOk

`func (o *CommandHistory) GetDurationMsOk() (*map[string]interface{}, bool)`

GetDurationMsOk returns a tuple with the DurationMs field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDurationMs

`func (o *CommandHistory) SetDurationMs(v map[string]interface{})`

SetDurationMs sets DurationMs field to given value.


### GetRiskLevel

`func (o *CommandHistory) GetRiskLevel() map[string]interface{}`

GetRiskLevel returns the RiskLevel field if non-nil, zero value otherwise.

### GetRiskLevelOk

`func (o *CommandHistory) GetRiskLevelOk() (*map[string]interface{}, bool)`

GetRiskLevelOk returns a tuple with the RiskLevel field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRiskLevel

`func (o *CommandHistory) SetRiskLevel(v map[string]interface{})`

SetRiskLevel sets RiskLevel field to given value.


### GetSuccess

`func (o *CommandHistory) GetSuccess() bool`

GetSuccess returns the Success field if non-nil, zero value otherwise.

### GetSuccessOk

`func (o *CommandHistory) GetSuccessOk() (*bool, bool)`

GetSuccessOk returns a tuple with the Success field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSuccess

`func (o *CommandHistory) SetSuccess(v bool)`

SetSuccess sets Success field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


