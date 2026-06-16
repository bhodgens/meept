# CompleteRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**JobId** | **string** |  | 
**Resultomitempty** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewCompleteRequest

`func NewCompleteRequest(jobId string, ) *CompleteRequest`

NewCompleteRequest instantiates a new CompleteRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCompleteRequestWithDefaults

`func NewCompleteRequestWithDefaults() *CompleteRequest`

NewCompleteRequestWithDefaults instantiates a new CompleteRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetJobId

`func (o *CompleteRequest) GetJobId() string`

GetJobId returns the JobId field if non-nil, zero value otherwise.

### GetJobIdOk

`func (o *CompleteRequest) GetJobIdOk() (*string, bool)`

GetJobIdOk returns a tuple with the JobId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetJobId

`func (o *CompleteRequest) SetJobId(v string)`

SetJobId sets JobId field to given value.


### GetResultomitempty

`func (o *CompleteRequest) GetResultomitempty() map[string]interface{}`

GetResultomitempty returns the Resultomitempty field if non-nil, zero value otherwise.

### GetResultomitemptyOk

`func (o *CompleteRequest) GetResultomitemptyOk() (*map[string]interface{}, bool)`

GetResultomitemptyOk returns a tuple with the Resultomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetResultomitempty

`func (o *CompleteRequest) SetResultomitempty(v map[string]interface{})`

SetResultomitempty sets Resultomitempty field to given value.

### HasResultomitempty

`func (o *CompleteRequest) HasResultomitempty() bool`

HasResultomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


