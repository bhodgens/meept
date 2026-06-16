# TemplatesInvokeResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Prompt** | **string** |  | 
**Outputomitempty** | Pointer to **string** |  | [optional] 
**Success** | **bool** |  | 
**Erroromitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewTemplatesInvokeResult

`func NewTemplatesInvokeResult(prompt string, success bool, ) *TemplatesInvokeResult`

NewTemplatesInvokeResult instantiates a new TemplatesInvokeResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewTemplatesInvokeResultWithDefaults

`func NewTemplatesInvokeResultWithDefaults() *TemplatesInvokeResult`

NewTemplatesInvokeResultWithDefaults instantiates a new TemplatesInvokeResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetPrompt

`func (o *TemplatesInvokeResult) GetPrompt() string`

GetPrompt returns the Prompt field if non-nil, zero value otherwise.

### GetPromptOk

`func (o *TemplatesInvokeResult) GetPromptOk() (*string, bool)`

GetPromptOk returns a tuple with the Prompt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPrompt

`func (o *TemplatesInvokeResult) SetPrompt(v string)`

SetPrompt sets Prompt field to given value.


### GetOutputomitempty

`func (o *TemplatesInvokeResult) GetOutputomitempty() string`

GetOutputomitempty returns the Outputomitempty field if non-nil, zero value otherwise.

### GetOutputomitemptyOk

`func (o *TemplatesInvokeResult) GetOutputomitemptyOk() (*string, bool)`

GetOutputomitemptyOk returns a tuple with the Outputomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetOutputomitempty

`func (o *TemplatesInvokeResult) SetOutputomitempty(v string)`

SetOutputomitempty sets Outputomitempty field to given value.

### HasOutputomitempty

`func (o *TemplatesInvokeResult) HasOutputomitempty() bool`

HasOutputomitempty returns a boolean if a field has been set.

### GetSuccess

`func (o *TemplatesInvokeResult) GetSuccess() bool`

GetSuccess returns the Success field if non-nil, zero value otherwise.

### GetSuccessOk

`func (o *TemplatesInvokeResult) GetSuccessOk() (*bool, bool)`

GetSuccessOk returns a tuple with the Success field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSuccess

`func (o *TemplatesInvokeResult) SetSuccess(v bool)`

SetSuccess sets Success field to given value.


### GetErroromitempty

`func (o *TemplatesInvokeResult) GetErroromitempty() string`

GetErroromitempty returns the Erroromitempty field if non-nil, zero value otherwise.

### GetErroromitemptyOk

`func (o *TemplatesInvokeResult) GetErroromitemptyOk() (*string, bool)`

GetErroromitemptyOk returns a tuple with the Erroromitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetErroromitempty

`func (o *TemplatesInvokeResult) SetErroromitempty(v string)`

SetErroromitempty sets Erroromitempty field to given value.

### HasErroromitempty

`func (o *TemplatesInvokeResult) HasErroromitempty() bool`

HasErroromitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


