# CheckRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Action** | **string** |  | 
**Resource** | **string** |  | 

## Methods

### NewCheckRequest

`func NewCheckRequest(action string, resource string, ) *CheckRequest`

NewCheckRequest instantiates a new CheckRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCheckRequestWithDefaults

`func NewCheckRequestWithDefaults() *CheckRequest`

NewCheckRequestWithDefaults instantiates a new CheckRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetAction

`func (o *CheckRequest) GetAction() string`

GetAction returns the Action field if non-nil, zero value otherwise.

### GetActionOk

`func (o *CheckRequest) GetActionOk() (*string, bool)`

GetActionOk returns a tuple with the Action field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAction

`func (o *CheckRequest) SetAction(v string)`

SetAction sets Action field to given value.


### GetResource

`func (o *CheckRequest) GetResource() string`

GetResource returns the Resource field if non-nil, zero value otherwise.

### GetResourceOk

`func (o *CheckRequest) GetResourceOk() (*string, bool)`

GetResourceOk returns a tuple with the Resource field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetResource

`func (o *CheckRequest) SetResource(v string)`

SetResource sets Resource field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


