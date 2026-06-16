# EnqueueRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Type** | **string** |  | 
**Priorityomitempty** | Pointer to **int32** |  | [optional] 
**TaskIdomitempty** | Pointer to **string** |  | [optional] 
**Prompt** | **string** |  | 
**SessionIdomitempty** | Pointer to **string** |  | [optional] 
**RequiredCapsomitempty** | Pointer to **NullableString** |  | [optional] 
**Payloadomitempty** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewEnqueueRequest

`func NewEnqueueRequest(type_ string, prompt string, ) *EnqueueRequest`

NewEnqueueRequest instantiates a new EnqueueRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewEnqueueRequestWithDefaults

`func NewEnqueueRequestWithDefaults() *EnqueueRequest`

NewEnqueueRequestWithDefaults instantiates a new EnqueueRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetType

`func (o *EnqueueRequest) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *EnqueueRequest) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *EnqueueRequest) SetType(v string)`

SetType sets Type field to given value.


### GetPriorityomitempty

`func (o *EnqueueRequest) GetPriorityomitempty() int32`

GetPriorityomitempty returns the Priorityomitempty field if non-nil, zero value otherwise.

### GetPriorityomitemptyOk

`func (o *EnqueueRequest) GetPriorityomitemptyOk() (*int32, bool)`

GetPriorityomitemptyOk returns a tuple with the Priorityomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPriorityomitempty

`func (o *EnqueueRequest) SetPriorityomitempty(v int32)`

SetPriorityomitempty sets Priorityomitempty field to given value.

### HasPriorityomitempty

`func (o *EnqueueRequest) HasPriorityomitempty() bool`

HasPriorityomitempty returns a boolean if a field has been set.

### GetTaskIdomitempty

`func (o *EnqueueRequest) GetTaskIdomitempty() string`

GetTaskIdomitempty returns the TaskIdomitempty field if non-nil, zero value otherwise.

### GetTaskIdomitemptyOk

`func (o *EnqueueRequest) GetTaskIdomitemptyOk() (*string, bool)`

GetTaskIdomitemptyOk returns a tuple with the TaskIdomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTaskIdomitempty

`func (o *EnqueueRequest) SetTaskIdomitempty(v string)`

SetTaskIdomitempty sets TaskIdomitempty field to given value.

### HasTaskIdomitempty

`func (o *EnqueueRequest) HasTaskIdomitempty() bool`

HasTaskIdomitempty returns a boolean if a field has been set.

### GetPrompt

`func (o *EnqueueRequest) GetPrompt() string`

GetPrompt returns the Prompt field if non-nil, zero value otherwise.

### GetPromptOk

`func (o *EnqueueRequest) GetPromptOk() (*string, bool)`

GetPromptOk returns a tuple with the Prompt field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPrompt

`func (o *EnqueueRequest) SetPrompt(v string)`

SetPrompt sets Prompt field to given value.


### GetSessionIdomitempty

`func (o *EnqueueRequest) GetSessionIdomitempty() string`

GetSessionIdomitempty returns the SessionIdomitempty field if non-nil, zero value otherwise.

### GetSessionIdomitemptyOk

`func (o *EnqueueRequest) GetSessionIdomitemptyOk() (*string, bool)`

GetSessionIdomitemptyOk returns a tuple with the SessionIdomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSessionIdomitempty

`func (o *EnqueueRequest) SetSessionIdomitempty(v string)`

SetSessionIdomitempty sets SessionIdomitempty field to given value.

### HasSessionIdomitempty

`func (o *EnqueueRequest) HasSessionIdomitempty() bool`

HasSessionIdomitempty returns a boolean if a field has been set.

### GetRequiredCapsomitempty

`func (o *EnqueueRequest) GetRequiredCapsomitempty() string`

GetRequiredCapsomitempty returns the RequiredCapsomitempty field if non-nil, zero value otherwise.

### GetRequiredCapsomitemptyOk

`func (o *EnqueueRequest) GetRequiredCapsomitemptyOk() (*string, bool)`

GetRequiredCapsomitemptyOk returns a tuple with the RequiredCapsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRequiredCapsomitempty

`func (o *EnqueueRequest) SetRequiredCapsomitempty(v string)`

SetRequiredCapsomitempty sets RequiredCapsomitempty field to given value.

### HasRequiredCapsomitempty

`func (o *EnqueueRequest) HasRequiredCapsomitempty() bool`

HasRequiredCapsomitempty returns a boolean if a field has been set.

### SetRequiredCapsomitemptyNil

`func (o *EnqueueRequest) SetRequiredCapsomitemptyNil(b bool)`

 SetRequiredCapsomitemptyNil sets the value for RequiredCapsomitempty to be an explicit nil

### UnsetRequiredCapsomitempty
`func (o *EnqueueRequest) UnsetRequiredCapsomitempty()`

UnsetRequiredCapsomitempty ensures that no value is present for RequiredCapsomitempty, not even an explicit nil
### GetPayloadomitempty

`func (o *EnqueueRequest) GetPayloadomitempty() string`

GetPayloadomitempty returns the Payloadomitempty field if non-nil, zero value otherwise.

### GetPayloadomitemptyOk

`func (o *EnqueueRequest) GetPayloadomitemptyOk() (*string, bool)`

GetPayloadomitemptyOk returns a tuple with the Payloadomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPayloadomitempty

`func (o *EnqueueRequest) SetPayloadomitempty(v string)`

SetPayloadomitempty sets Payloadomitempty field to given value.

### HasPayloadomitempty

`func (o *EnqueueRequest) HasPayloadomitempty() bool`

HasPayloadomitempty returns a boolean if a field has been set.

### SetPayloadomitemptyNil

`func (o *EnqueueRequest) SetPayloadomitemptyNil(b bool)`

 SetPayloadomitemptyNil sets the value for Payloadomitempty to be an explicit nil

### UnsetPayloadomitempty
`func (o *EnqueueRequest) UnsetPayloadomitempty()`

UnsetPayloadomitempty ensures that no value is present for Payloadomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


