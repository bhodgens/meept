//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'calendar_service.g.dart';

/// CalendarService
///
/// Properties:
/// * [client] 
@BuiltValue()
abstract class CalendarService implements Built<CalendarService, CalendarServiceBuilder> {
  @BuiltValueField(wireName: r'client')
  JsonObject? get client;

  CalendarService._();

  factory CalendarService([void updates(CalendarServiceBuilder b)]) = _$CalendarService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CalendarServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CalendarService> get serializer => _$CalendarServiceSerializer();
}

class _$CalendarServiceSerializer implements PrimitiveSerializer<CalendarService> {
  @override
  final Iterable<Type> types = const [CalendarService, _$CalendarService];

  @override
  final String wireName = r'CalendarService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CalendarService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.client != null) {
      yield r'client';
      yield serializers.serialize(
        object.client,
        specifiedType: const FullType.nullable(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CalendarService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CalendarServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'client':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(JsonObject),
          ) as JsonObject?;
          if (valueDes == null) continue;
          result.client = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CalendarService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CalendarServiceBuilder();
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

