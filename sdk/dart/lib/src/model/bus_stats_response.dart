//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'bus_stats_response.g.dart';

/// BusStatsResponse
///
/// Properties:
/// * [subscribers] 
/// * [messagesSent] 
/// * [queuedMessages] 
@BuiltValue()
abstract class BusStatsResponse implements Built<BusStatsResponse, BusStatsResponseBuilder> {
  @BuiltValueField(wireName: r'subscribers')
  int get subscribers;

  @BuiltValueField(wireName: r'messages_sent')
  int get messagesSent;

  @BuiltValueField(wireName: r'queued_messages')
  int get queuedMessages;

  BusStatsResponse._();

  factory BusStatsResponse([void updates(BusStatsResponseBuilder b)]) = _$BusStatsResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(BusStatsResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<BusStatsResponse> get serializer => _$BusStatsResponseSerializer();
}

class _$BusStatsResponseSerializer implements PrimitiveSerializer<BusStatsResponse> {
  @override
  final Iterable<Type> types = const [BusStatsResponse, _$BusStatsResponse];

  @override
  final String wireName = r'BusStatsResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    BusStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'subscribers';
    yield serializers.serialize(
      object.subscribers,
      specifiedType: const FullType(int),
    );
    yield r'messages_sent';
    yield serializers.serialize(
      object.messagesSent,
      specifiedType: const FullType(int),
    );
    yield r'queued_messages';
    yield serializers.serialize(
      object.queuedMessages,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    BusStatsResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required BusStatsResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'subscribers':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.subscribers = valueDes;
          break;
        case r'messages_sent':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.messagesSent = valueDes;
          break;
        case r'queued_messages':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.queuedMessages = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  BusStatsResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = BusStatsResponseBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

