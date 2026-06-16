# DaemonStatus

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Status** | **string** |  | 
**Pidomitempty** | Pointer to **int32** |  | [optional] 
**UptimeSecondsomitempty** | Pointer to **float32** |  | [optional] 
**Modelomitempty** | Pointer to **string** |  | [optional] 
**TokensUsed** | **int32** |  | 
**TokensRemaining** | **int32** |  | 
**BudgetUsed** | **float32** |  | 
**BudgetRemaining** | **float32** |  | 
**HourlyUsed** | Pointer to **int32** |  | [optional] 
**HourlyRemaining** | Pointer to **int32** |  | [optional] 
**DailyUsed** | Pointer to **int32** |  | [optional] 
**DailyRemaining** | Pointer to **int32** |  | [optional] 
**RpmCurrent** | Pointer to **int32** |  | [optional] 
**RpmLimit** | Pointer to **int32** |  | [optional] 
**DailyCostUsed** | Pointer to **float32** |  | [optional] 
**DailyCostLimit** | Pointer to **float32** |  | [optional] 
**HourlyCostUsed** | Pointer to **float32** |  | [optional] 
**HourlyCostLimit** | Pointer to **float32** |  | [optional] 
**PerTaskCost** | Pointer to **float32** |  | [optional] 
**PerTaskBudget** | Pointer to **int32** |  | [optional] 
**PerSessionCost** | Pointer to **float32** |  | [optional] 
**PerSessionBudget** | Pointer to **int32** |  | [optional] 
**RegisteredMethods** | **int32** |  | 
**BusSubscribers** | **int32** |  | 

## Methods

### NewDaemonStatus

`func NewDaemonStatus(status string, tokensUsed int32, tokensRemaining int32, budgetUsed float32, budgetRemaining float32, registeredMethods int32, busSubscribers int32, ) *DaemonStatus`

NewDaemonStatus instantiates a new DaemonStatus object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewDaemonStatusWithDefaults

`func NewDaemonStatusWithDefaults() *DaemonStatus`

NewDaemonStatusWithDefaults instantiates a new DaemonStatus object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetStatus

`func (o *DaemonStatus) GetStatus() string`

GetStatus returns the Status field if non-nil, zero value otherwise.

### GetStatusOk

`func (o *DaemonStatus) GetStatusOk() (*string, bool)`

GetStatusOk returns a tuple with the Status field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStatus

`func (o *DaemonStatus) SetStatus(v string)`

SetStatus sets Status field to given value.


### GetPidomitempty

`func (o *DaemonStatus) GetPidomitempty() int32`

GetPidomitempty returns the Pidomitempty field if non-nil, zero value otherwise.

### GetPidomitemptyOk

`func (o *DaemonStatus) GetPidomitemptyOk() (*int32, bool)`

GetPidomitemptyOk returns a tuple with the Pidomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPidomitempty

`func (o *DaemonStatus) SetPidomitempty(v int32)`

SetPidomitempty sets Pidomitempty field to given value.

### HasPidomitempty

`func (o *DaemonStatus) HasPidomitempty() bool`

HasPidomitempty returns a boolean if a field has been set.

### GetUptimeSecondsomitempty

`func (o *DaemonStatus) GetUptimeSecondsomitempty() float32`

GetUptimeSecondsomitempty returns the UptimeSecondsomitempty field if non-nil, zero value otherwise.

### GetUptimeSecondsomitemptyOk

`func (o *DaemonStatus) GetUptimeSecondsomitemptyOk() (*float32, bool)`

GetUptimeSecondsomitemptyOk returns a tuple with the UptimeSecondsomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetUptimeSecondsomitempty

`func (o *DaemonStatus) SetUptimeSecondsomitempty(v float32)`

SetUptimeSecondsomitempty sets UptimeSecondsomitempty field to given value.

### HasUptimeSecondsomitempty

`func (o *DaemonStatus) HasUptimeSecondsomitempty() bool`

HasUptimeSecondsomitempty returns a boolean if a field has been set.

### GetModelomitempty

`func (o *DaemonStatus) GetModelomitempty() string`

GetModelomitempty returns the Modelomitempty field if non-nil, zero value otherwise.

### GetModelomitemptyOk

`func (o *DaemonStatus) GetModelomitemptyOk() (*string, bool)`

GetModelomitemptyOk returns a tuple with the Modelomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetModelomitempty

`func (o *DaemonStatus) SetModelomitempty(v string)`

SetModelomitempty sets Modelomitempty field to given value.

### HasModelomitempty

`func (o *DaemonStatus) HasModelomitempty() bool`

HasModelomitempty returns a boolean if a field has been set.

### GetTokensUsed

`func (o *DaemonStatus) GetTokensUsed() int32`

GetTokensUsed returns the TokensUsed field if non-nil, zero value otherwise.

### GetTokensUsedOk

`func (o *DaemonStatus) GetTokensUsedOk() (*int32, bool)`

GetTokensUsedOk returns a tuple with the TokensUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTokensUsed

`func (o *DaemonStatus) SetTokensUsed(v int32)`

SetTokensUsed sets TokensUsed field to given value.


### GetTokensRemaining

`func (o *DaemonStatus) GetTokensRemaining() int32`

GetTokensRemaining returns the TokensRemaining field if non-nil, zero value otherwise.

### GetTokensRemainingOk

`func (o *DaemonStatus) GetTokensRemainingOk() (*int32, bool)`

GetTokensRemainingOk returns a tuple with the TokensRemaining field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTokensRemaining

`func (o *DaemonStatus) SetTokensRemaining(v int32)`

SetTokensRemaining sets TokensRemaining field to given value.


### GetBudgetUsed

`func (o *DaemonStatus) GetBudgetUsed() float32`

GetBudgetUsed returns the BudgetUsed field if non-nil, zero value otherwise.

### GetBudgetUsedOk

`func (o *DaemonStatus) GetBudgetUsedOk() (*float32, bool)`

GetBudgetUsedOk returns a tuple with the BudgetUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBudgetUsed

`func (o *DaemonStatus) SetBudgetUsed(v float32)`

SetBudgetUsed sets BudgetUsed field to given value.


### GetBudgetRemaining

`func (o *DaemonStatus) GetBudgetRemaining() float32`

GetBudgetRemaining returns the BudgetRemaining field if non-nil, zero value otherwise.

### GetBudgetRemainingOk

`func (o *DaemonStatus) GetBudgetRemainingOk() (*float32, bool)`

GetBudgetRemainingOk returns a tuple with the BudgetRemaining field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBudgetRemaining

`func (o *DaemonStatus) SetBudgetRemaining(v float32)`

SetBudgetRemaining sets BudgetRemaining field to given value.


### GetHourlyUsed

`func (o *DaemonStatus) GetHourlyUsed() int32`

GetHourlyUsed returns the HourlyUsed field if non-nil, zero value otherwise.

### GetHourlyUsedOk

`func (o *DaemonStatus) GetHourlyUsedOk() (*int32, bool)`

GetHourlyUsedOk returns a tuple with the HourlyUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHourlyUsed

`func (o *DaemonStatus) SetHourlyUsed(v int32)`

SetHourlyUsed sets HourlyUsed field to given value.

### HasHourlyUsed

`func (o *DaemonStatus) HasHourlyUsed() bool`

HasHourlyUsed returns a boolean if a field has been set.

### GetHourlyRemaining

`func (o *DaemonStatus) GetHourlyRemaining() int32`

GetHourlyRemaining returns the HourlyRemaining field if non-nil, zero value otherwise.

### GetHourlyRemainingOk

`func (o *DaemonStatus) GetHourlyRemainingOk() (*int32, bool)`

GetHourlyRemainingOk returns a tuple with the HourlyRemaining field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHourlyRemaining

`func (o *DaemonStatus) SetHourlyRemaining(v int32)`

SetHourlyRemaining sets HourlyRemaining field to given value.

### HasHourlyRemaining

`func (o *DaemonStatus) HasHourlyRemaining() bool`

HasHourlyRemaining returns a boolean if a field has been set.

### GetDailyUsed

`func (o *DaemonStatus) GetDailyUsed() int32`

GetDailyUsed returns the DailyUsed field if non-nil, zero value otherwise.

### GetDailyUsedOk

`func (o *DaemonStatus) GetDailyUsedOk() (*int32, bool)`

GetDailyUsedOk returns a tuple with the DailyUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDailyUsed

`func (o *DaemonStatus) SetDailyUsed(v int32)`

SetDailyUsed sets DailyUsed field to given value.

### HasDailyUsed

`func (o *DaemonStatus) HasDailyUsed() bool`

HasDailyUsed returns a boolean if a field has been set.

### GetDailyRemaining

`func (o *DaemonStatus) GetDailyRemaining() int32`

GetDailyRemaining returns the DailyRemaining field if non-nil, zero value otherwise.

### GetDailyRemainingOk

`func (o *DaemonStatus) GetDailyRemainingOk() (*int32, bool)`

GetDailyRemainingOk returns a tuple with the DailyRemaining field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDailyRemaining

`func (o *DaemonStatus) SetDailyRemaining(v int32)`

SetDailyRemaining sets DailyRemaining field to given value.

### HasDailyRemaining

`func (o *DaemonStatus) HasDailyRemaining() bool`

HasDailyRemaining returns a boolean if a field has been set.

### GetRpmCurrent

`func (o *DaemonStatus) GetRpmCurrent() int32`

GetRpmCurrent returns the RpmCurrent field if non-nil, zero value otherwise.

### GetRpmCurrentOk

`func (o *DaemonStatus) GetRpmCurrentOk() (*int32, bool)`

GetRpmCurrentOk returns a tuple with the RpmCurrent field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRpmCurrent

`func (o *DaemonStatus) SetRpmCurrent(v int32)`

SetRpmCurrent sets RpmCurrent field to given value.

### HasRpmCurrent

`func (o *DaemonStatus) HasRpmCurrent() bool`

HasRpmCurrent returns a boolean if a field has been set.

### GetRpmLimit

`func (o *DaemonStatus) GetRpmLimit() int32`

GetRpmLimit returns the RpmLimit field if non-nil, zero value otherwise.

### GetRpmLimitOk

`func (o *DaemonStatus) GetRpmLimitOk() (*int32, bool)`

GetRpmLimitOk returns a tuple with the RpmLimit field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRpmLimit

`func (o *DaemonStatus) SetRpmLimit(v int32)`

SetRpmLimit sets RpmLimit field to given value.

### HasRpmLimit

`func (o *DaemonStatus) HasRpmLimit() bool`

HasRpmLimit returns a boolean if a field has been set.

### GetDailyCostUsed

`func (o *DaemonStatus) GetDailyCostUsed() float32`

GetDailyCostUsed returns the DailyCostUsed field if non-nil, zero value otherwise.

### GetDailyCostUsedOk

`func (o *DaemonStatus) GetDailyCostUsedOk() (*float32, bool)`

GetDailyCostUsedOk returns a tuple with the DailyCostUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDailyCostUsed

`func (o *DaemonStatus) SetDailyCostUsed(v float32)`

SetDailyCostUsed sets DailyCostUsed field to given value.

### HasDailyCostUsed

`func (o *DaemonStatus) HasDailyCostUsed() bool`

HasDailyCostUsed returns a boolean if a field has been set.

### GetDailyCostLimit

`func (o *DaemonStatus) GetDailyCostLimit() float32`

GetDailyCostLimit returns the DailyCostLimit field if non-nil, zero value otherwise.

### GetDailyCostLimitOk

`func (o *DaemonStatus) GetDailyCostLimitOk() (*float32, bool)`

GetDailyCostLimitOk returns a tuple with the DailyCostLimit field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDailyCostLimit

`func (o *DaemonStatus) SetDailyCostLimit(v float32)`

SetDailyCostLimit sets DailyCostLimit field to given value.

### HasDailyCostLimit

`func (o *DaemonStatus) HasDailyCostLimit() bool`

HasDailyCostLimit returns a boolean if a field has been set.

### GetHourlyCostUsed

`func (o *DaemonStatus) GetHourlyCostUsed() float32`

GetHourlyCostUsed returns the HourlyCostUsed field if non-nil, zero value otherwise.

### GetHourlyCostUsedOk

`func (o *DaemonStatus) GetHourlyCostUsedOk() (*float32, bool)`

GetHourlyCostUsedOk returns a tuple with the HourlyCostUsed field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHourlyCostUsed

`func (o *DaemonStatus) SetHourlyCostUsed(v float32)`

SetHourlyCostUsed sets HourlyCostUsed field to given value.

### HasHourlyCostUsed

`func (o *DaemonStatus) HasHourlyCostUsed() bool`

HasHourlyCostUsed returns a boolean if a field has been set.

### GetHourlyCostLimit

`func (o *DaemonStatus) GetHourlyCostLimit() float32`

GetHourlyCostLimit returns the HourlyCostLimit field if non-nil, zero value otherwise.

### GetHourlyCostLimitOk

`func (o *DaemonStatus) GetHourlyCostLimitOk() (*float32, bool)`

GetHourlyCostLimitOk returns a tuple with the HourlyCostLimit field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHourlyCostLimit

`func (o *DaemonStatus) SetHourlyCostLimit(v float32)`

SetHourlyCostLimit sets HourlyCostLimit field to given value.

### HasHourlyCostLimit

`func (o *DaemonStatus) HasHourlyCostLimit() bool`

HasHourlyCostLimit returns a boolean if a field has been set.

### GetPerTaskCost

`func (o *DaemonStatus) GetPerTaskCost() float32`

GetPerTaskCost returns the PerTaskCost field if non-nil, zero value otherwise.

### GetPerTaskCostOk

`func (o *DaemonStatus) GetPerTaskCostOk() (*float32, bool)`

GetPerTaskCostOk returns a tuple with the PerTaskCost field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPerTaskCost

`func (o *DaemonStatus) SetPerTaskCost(v float32)`

SetPerTaskCost sets PerTaskCost field to given value.

### HasPerTaskCost

`func (o *DaemonStatus) HasPerTaskCost() bool`

HasPerTaskCost returns a boolean if a field has been set.

### GetPerTaskBudget

`func (o *DaemonStatus) GetPerTaskBudget() int32`

GetPerTaskBudget returns the PerTaskBudget field if non-nil, zero value otherwise.

### GetPerTaskBudgetOk

`func (o *DaemonStatus) GetPerTaskBudgetOk() (*int32, bool)`

GetPerTaskBudgetOk returns a tuple with the PerTaskBudget field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPerTaskBudget

`func (o *DaemonStatus) SetPerTaskBudget(v int32)`

SetPerTaskBudget sets PerTaskBudget field to given value.

### HasPerTaskBudget

`func (o *DaemonStatus) HasPerTaskBudget() bool`

HasPerTaskBudget returns a boolean if a field has been set.

### GetPerSessionCost

`func (o *DaemonStatus) GetPerSessionCost() float32`

GetPerSessionCost returns the PerSessionCost field if non-nil, zero value otherwise.

### GetPerSessionCostOk

`func (o *DaemonStatus) GetPerSessionCostOk() (*float32, bool)`

GetPerSessionCostOk returns a tuple with the PerSessionCost field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPerSessionCost

`func (o *DaemonStatus) SetPerSessionCost(v float32)`

SetPerSessionCost sets PerSessionCost field to given value.

### HasPerSessionCost

`func (o *DaemonStatus) HasPerSessionCost() bool`

HasPerSessionCost returns a boolean if a field has been set.

### GetPerSessionBudget

`func (o *DaemonStatus) GetPerSessionBudget() int32`

GetPerSessionBudget returns the PerSessionBudget field if non-nil, zero value otherwise.

### GetPerSessionBudgetOk

`func (o *DaemonStatus) GetPerSessionBudgetOk() (*int32, bool)`

GetPerSessionBudgetOk returns a tuple with the PerSessionBudget field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetPerSessionBudget

`func (o *DaemonStatus) SetPerSessionBudget(v int32)`

SetPerSessionBudget sets PerSessionBudget field to given value.

### HasPerSessionBudget

`func (o *DaemonStatus) HasPerSessionBudget() bool`

HasPerSessionBudget returns a boolean if a field has been set.

### GetRegisteredMethods

`func (o *DaemonStatus) GetRegisteredMethods() int32`

GetRegisteredMethods returns the RegisteredMethods field if non-nil, zero value otherwise.

### GetRegisteredMethodsOk

`func (o *DaemonStatus) GetRegisteredMethodsOk() (*int32, bool)`

GetRegisteredMethodsOk returns a tuple with the RegisteredMethods field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRegisteredMethods

`func (o *DaemonStatus) SetRegisteredMethods(v int32)`

SetRegisteredMethods sets RegisteredMethods field to given value.


### GetBusSubscribers

`func (o *DaemonStatus) GetBusSubscribers() int32`

GetBusSubscribers returns the BusSubscribers field if non-nil, zero value otherwise.

### GetBusSubscribersOk

`func (o *DaemonStatus) GetBusSubscribersOk() (*int32, bool)`

GetBusSubscribersOk returns a tuple with the BusSubscribers field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetBusSubscribers

`func (o *DaemonStatus) SetBusSubscribers(v int32)`

SetBusSubscribers sets BusSubscribers field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


