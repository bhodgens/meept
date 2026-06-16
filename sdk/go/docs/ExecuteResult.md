# ExecuteResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Output** | **string** |  | 
**Success** | **bool** |  | 
**Erroromitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewExecuteResult

`func NewExecuteResult(output string, success bool, ) *ExecuteResult`

NewExecuteResult instantiates a new ExecuteResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewExecuteResultWithDefaults

`func NewExecuteResultWithDefaults() *ExecuteResult`

NewExecuteResultWithDefaults instantiates a new ExecuteResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetOutput

`func (o *ExecuteResult) GetOutput() string`

GetOutput returns the Output field if non-nil, zero value otherwise.

### GetOutputOk

`func (o *ExecuteResult) GetOutputOk() (*string, bool)`

GetOutputOk returns a tuple with the Output field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOutput

`func (o *ExecuteResult) SetOutput(v string)`

SetOutput sets Output field to given value.


### GetSuccess

`func (o *ExecuteResult) GetSuccess() bool`

GetSuccess returns the Success field if non-nil, zero value otherwise.

### GetSuccessOk

`func (o *ExecuteResult) GetSuccessOk() (*bool, bool)`

GetSuccessOk returns a tuple with the Success field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSuccess

`func (o *ExecuteResult) SetSuccess(v bool)`

SetSuccess sets Success field to given value.


### GetErroromitempty

`func (o *ExecuteResult) GetErroromitempty() string`

GetErroromitempty returns the Erroromitempty field if non-nil, zero value otherwise.

### GetErroromitemptyOk

`func (o *ExecuteResult) GetErroromitemptyOk() (*string, bool)`

GetErroromitemptyOk returns a tuple with the Erroromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErroromitempty

`func (o *ExecuteResult) SetErroromitempty(v string)`

SetErroromitempty sets Erroromitempty field to given value.

### HasErroromitempty

`func (o *ExecuteResult) HasErroromitempty() bool`

HasErroromitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


