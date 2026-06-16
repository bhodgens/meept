# BranchSessionRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** |  | 
**TargetMessageId** | **int32** |  | 

## Methods

### NewBranchSessionRequest

`func NewBranchSessionRequest(id string, targetMessageId int32, ) *BranchSessionRequest`

NewBranchSessionRequest instantiates a new BranchSessionRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewBranchSessionRequestWithDefaults

`func NewBranchSessionRequestWithDefaults() *BranchSessionRequest`

NewBranchSessionRequestWithDefaults instantiates a new BranchSessionRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetId

`func (o *BranchSessionRequest) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *BranchSessionRequest) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *BranchSessionRequest) SetId(v string)`

SetId sets Id field to given value.


### GetTargetMessageId

`func (o *BranchSessionRequest) GetTargetMessageId() int32`

GetTargetMessageId returns the TargetMessageId field if non-nil, zero value otherwise.

### GetTargetMessageIdOk

`func (o *BranchSessionRequest) GetTargetMessageIdOk() (*int32, bool)`

GetTargetMessageIdOk returns a tuple with the TargetMessageId field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTargetMessageId

`func (o *BranchSessionRequest) SetTargetMessageId(v int32)`

SetTargetMessageId sets TargetMessageId field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


