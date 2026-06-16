# RetryRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**JobId** | **string** |  | 

## Methods

### NewRetryRequest

`func NewRetryRequest(jobId string, ) *RetryRequest`

NewRetryRequest instantiates a new RetryRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewRetryRequestWithDefaults

`func NewRetryRequestWithDefaults() *RetryRequest`

NewRetryRequestWithDefaults instantiates a new RetryRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetJobId

`func (o *RetryRequest) GetJobId() string`

GetJobId returns the JobId field if non-nil, zero value otherwise.

### GetJobIdOk

`func (o *RetryRequest) GetJobIdOk() (*string, bool)`

GetJobIdOk returns a tuple with the JobId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetJobId

`func (o *RetryRequest) SetJobId(v string)`

SetJobId sets JobId field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


