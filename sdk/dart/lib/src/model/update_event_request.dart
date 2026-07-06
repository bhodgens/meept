//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'update_event_request.g.dart';

/// UpdateEventRequest
///
/// Properties:
/// * [id] 
/// * [summary] 
/// * [description] 
/// * [location] 
/// * [start] 
/// * [end] 
@BuiltValue()
abstract class UpdateEventRequest implements Built<UpdateEventRequest, UpdateEventRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'summary')
  String? get summary;

  @BuiltValueField(wireName: r'description')
  String? get description;

  @BuiltValueField(wireName: r'location')
  String? get location;

  @BuiltValueField(wireName: r'start')
  String? get start;

  @BuiltValueField(wireName: r'end')
  String? get end;

  UpdateEventRequest._();

  factory UpdateEventRequest([void updates(UpdateEventRequestBuilder b)]) = _$UpdateEventRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UpdateEventRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UpdateEventRequest> get serializer => _$UpdateEventRequestSerializer();
}

class _$UpdateEventRequestSerializer implements PrimitiveSerializer<UpdateEventRequest> {
  @override
  final Iterable<Type> types = const [UpdateEventRequest, _$UpdateEventRequest];

  @override
  final String wireName = r'UpdateEventRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UpdateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    if (object.summary != null) {
      yield r'summary';
      yield serializers.serialize(
        object.summary,
        specifiedType: const FullType(String),
      );
    }
    if (object.description != null) {
      yield r'description';
      yield serializers.serialize(
        object.description,
        specifiedType: const FullType(String),
      );
    }
    if (object.location != null) {
      yield r'location';
      yield serializers.serialize(
        object.location,
        specifiedType: const FullType(String),
      );
    }
    if (object.start != null) {
      yield r'start';
      yield serializers.serialize(
        object.start,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.end != null) {
      yield r'end';
      yield serializers.serialize(
        object.end,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    UpdateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UpdateEventRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'summary':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.summary = valueDes;
          break;
        case r'description':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.description = valueDes;
          break;
        case r'location':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.location = valueDes;
          break;
        case r'start':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.start = valueDes;
          break;
        case r'end':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.end = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UpdateEventRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UpdateEventRequestBuilder();
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

