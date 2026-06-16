# BusStatsResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Subscribers** | **int32** |  | 
**MessagesSent** | **int32** |  | 
**QueuedMessages** | **int32** |  | 

## Methods

### NewBusStatsResponse

`func NewBusStatsResponse(subscribers int32, messagesSent int32, queuedMessages int32, ) *BusStatsResponse`

NewBusStatsResponse instantiates a new BusStatsResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewBusStatsResponseWithDefaults

`func NewBusStatsResponseWithDefaults() *BusStatsResponse`

NewBusStatsResponseWithDefaults instantiates a new BusStatsResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSubscribers

`func (o *BusStatsResponse) GetSubscribers() int32`

GetSubscribers returns the Subscribers field if non-nil, zero value otherwise.

### GetSubscribersOk

`func (o *BusStatsResponse) GetSubscribersOk() (*int32, bool)`

GetSubscribersOk returns a tuple with the Subscribers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSubscribers

`func (o *BusStatsResponse) SetSubscribers(v int32)`

SetSubscribers sets Subscribers field to given value.


### GetMessagesSent

`func (o *BusStatsResponse) GetMessagesSent() int32`

GetMessagesSent returns the MessagesSent field if non-nil, zero value otherwise.

### GetMessagesSentOk

`func (o *BusStatsResponse) GetMessagesSentOk() (*int32, bool)`

GetMessagesSentOk returns a tuple with the MessagesSent field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMessagesSent

`func (o *BusStatsResponse) SetMessagesSent(v int32)`

SetMessagesSent sets MessagesSent field to given value.


### GetQueuedMessages

`func (o *BusStatsResponse) GetQueuedMessages() int32`

GetQueuedMessages returns the QueuedMessages field if non-nil, zero value otherwise.

### GetQueuedMessagesOk

`func (o *BusStatsResponse) GetQueuedMessagesOk() (*int32, bool)`

GetQueuedMessagesOk returns a tuple with the QueuedMessages field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetQueuedMessages

`func (o *BusStatsResponse) SetQueuedMessages(v int32)`

SetQueuedMessages sets QueuedMessages field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


