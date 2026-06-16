# ShellJobConfig

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Command** | **string** |  | 
**Argsomitempty** | Pointer to **NullableString** |  | [optional] 
**WorkDiromitempty** | Pointer to **string** |  | [optional] 
**Envomitempty** | Pointer to **NullableString** |  | [optional] 
**TimeoutSecsomitempty** | Pointer to **int32** |  | [optional] 
**CaptureOutput** | **bool** |  | 

## Methods

### NewShellJobConfig

`func NewShellJobConfig(command string, captureOutput bool, ) *ShellJobConfig`

NewShellJobConfig instantiates a new ShellJobConfig object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewShellJobConfigWithDefaults

`func NewShellJobConfigWithDefaults() *ShellJobConfig`

NewShellJobConfigWithDefaults instantiates a new ShellJobConfig object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetCommand

`func (o *ShellJobConfig) GetCommand() string`

GetCommand returns the Command field if non-nil, zero value otherwise.

### GetCommandOk

`func (o *ShellJobConfig) GetCommandOk() (*string, bool)`

GetCommandOk returns a tuple with the Command field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCommand

`func (o *ShellJobConfig) SetCommand(v string)`

SetCommand sets Command field to given value.


### GetArgsomitempty

`func (o *ShellJobConfig) GetArgsomitempty() string`

GetArgsomitempty returns the Argsomitempty field if non-nil, zero value otherwise.

### GetArgsomitemptyOk

`func (o *ShellJobConfig) GetArgsomitemptyOk() (*string, bool)`

GetArgsomitemptyOk returns a tuple with the Argsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetArgsomitempty

`func (o *ShellJobConfig) SetArgsomitempty(v string)`

SetArgsomitempty sets Argsomitempty field to given value.

### HasArgsomitempty

`func (o *ShellJobConfig) HasArgsomitempty() bool`

HasArgsomitempty returns a boolean if a field has been set.

### SetArgsomitemptyNil

`func (o *ShellJobConfig) SetArgsomitemptyNil(b bool)`

 SetArgsomitemptyNil sets the value for Argsomitempty to be an explicit nil

### UnsetArgsomitempty
`func (o *ShellJobConfig) UnsetArgsomitempty()`

UnsetArgsomitempty ensures that no value is present for Argsomitempty, not even an explicit nil
### GetWorkDiromitempty

`func (o *ShellJobConfig) GetWorkDiromitempty() string`

GetWorkDiromitempty returns the WorkDiromitempty field if non-nil, zero value otherwise.

### GetWorkDiromitemptyOk

`func (o *ShellJobConfig) GetWorkDiromitemptyOk() (*string, bool)`

GetWorkDiromitemptyOk returns a tuple with the WorkDiromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetWorkDiromitempty

`func (o *ShellJobConfig) SetWorkDiromitempty(v string)`

SetWorkDiromitempty sets WorkDiromitempty field to given value.

### HasWorkDiromitempty

`func (o *ShellJobConfig) HasWorkDiromitempty() bool`

HasWorkDiromitempty returns a boolean if a field has been set.

### GetEnvomitempty

`func (o *ShellJobConfig) GetEnvomitempty() string`

GetEnvomitempty returns the Envomitempty field if non-nil, zero value otherwise.

### GetEnvomitemptyOk

`func (o *ShellJobConfig) GetEnvomitemptyOk() (*string, bool)`

GetEnvomitemptyOk returns a tuple with the Envomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnvomitempty

`func (o *ShellJobConfig) SetEnvomitempty(v string)`

SetEnvomitempty sets Envomitempty field to given value.

### HasEnvomitempty

`func (o *ShellJobConfig) HasEnvomitempty() bool`

HasEnvomitempty returns a boolean if a field has been set.

### SetEnvomitemptyNil

`func (o *ShellJobConfig) SetEnvomitemptyNil(b bool)`

 SetEnvomitemptyNil sets the value for Envomitempty to be an explicit nil

### UnsetEnvomitempty
`func (o *ShellJobConfig) UnsetEnvomitempty()`

UnsetEnvomitempty ensures that no value is present for Envomitempty, not even an explicit nil
### GetTimeoutSecsomitempty

`func (o *ShellJobConfig) GetTimeoutSecsomitempty() int32`

GetTimeoutSecsomitempty returns the TimeoutSecsomitempty field if non-nil, zero value otherwise.

### GetTimeoutSecsomitemptyOk

`func (o *ShellJobConfig) GetTimeoutSecsomitemptyOk() (*int32, bool)`

GetTimeoutSecsomitemptyOk returns a tuple with the TimeoutSecsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTimeoutSecsomitempty

`func (o *ShellJobConfig) SetTimeoutSecsomitempty(v int32)`

SetTimeoutSecsomitempty sets TimeoutSecsomitempty field to given value.

### HasTimeoutSecsomitempty

`func (o *ShellJobConfig) HasTimeoutSecsomitempty() bool`

HasTimeoutSecsomitempty returns a boolean if a field has been set.

### GetCaptureOutput

`func (o *ShellJobConfig) GetCaptureOutput() bool`

GetCaptureOutput returns the CaptureOutput field if non-nil, zero value otherwise.

### GetCaptureOutputOk

`func (o *ShellJobConfig) GetCaptureOutputOk() (*bool, bool)`

GetCaptureOutputOk returns a tuple with the CaptureOutput field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCaptureOutput

`func (o *ShellJobConfig) SetCaptureOutput(v bool)`

SetCaptureOutput sets CaptureOutput field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


