# QueueStatusResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**SteeringDepth** | **int32** |  | 
**FollowupDepth** | **int32** |  | 
**IsActive** | **bool** |  | 
**Generation** | **int32** |  | 

## Methods

### NewQueueStatusResponse

`func NewQueueStatusResponse(steeringDepth int32, followupDepth int32, isActive bool, generation int32, ) *QueueStatusResponse`

NewQueueStatusResponse instantiates a new QueueStatusResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewQueueStatusResponseWithDefaults

`func NewQueueStatusResponseWithDefaults() *QueueStatusResponse`

NewQueueStatusResponseWithDefaults instantiates a new QueueStatusResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSteeringDepth

`func (o *QueueStatusResponse) GetSteeringDepth() int32`

GetSteeringDepth returns the SteeringDepth field if non-nil, zero value otherwise.

### GetSteeringDepthOk

`func (o *QueueStatusResponse) GetSteeringDepthOk() (*int32, bool)`

GetSteeringDepthOk returns a tuple with the SteeringDepth field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSteeringDepth

`func (o *QueueStatusResponse) SetSteeringDepth(v int32)`

SetSteeringDepth sets SteeringDepth field to given value.


### GetFollowupDepth

`func (o *QueueStatusResponse) GetFollowupDepth() int32`

GetFollowupDepth returns the FollowupDepth field if non-nil, zero value otherwise.

### GetFollowupDepthOk

`func (o *QueueStatusResponse) GetFollowupDepthOk() (*int32, bool)`

GetFollowupDepthOk returns a tuple with the FollowupDepth field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetFollowupDepth

`func (o *QueueStatusResponse) SetFollowupDepth(v int32)`

SetFollowupDepth sets FollowupDepth field to given value.


### GetIsActive

`func (o *QueueStatusResponse) GetIsActive() bool`

GetIsActive returns the IsActive field if non-nil, zero value otherwise.

### GetIsActiveOk

`func (o *QueueStatusResponse) GetIsActiveOk() (*bool, bool)`

GetIsActiveOk returns a tuple with the IsActive field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetIsActive

`func (o *QueueStatusResponse) SetIsActive(v bool)`

SetIsActive sets IsActive field to given value.


### GetGeneration

`func (o *QueueStatusResponse) GetGeneration() int32`

GetGeneration returns the Generation field if non-nil, zero value otherwise.

### GetGenerationOk

`func (o *QueueStatusResponse) GetGenerationOk() (*int32, bool)`

GetGenerationOk returns a tuple with the Generation field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetGeneration

`func (o *QueueStatusResponse) SetGeneration(v int32)`

SetGeneration sets Generation field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


