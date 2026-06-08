//
//  JSON5Normalizer.swift
//  MeeptMenuBar
//
//  Converts JSON5 to strict JSON by stripping comments, trailing commas,
//  and unquoted keys so JSONDecoder can parse it.
//

import Foundation

enum JSON5NormalizeError: Error {
    case unterminatedString
    case unterminatedBlockComment
}

/// Converts JSON5 input (comments, trailing commas, unquoted keys) to
/// strict JSON suitable for `JSONDecoder`.
struct JSON5Normalizer {

    /// Normalize JSON5 → strict JSON.
    static func normalize(_ input: String) throws -> String {
        var output = ""
        var i = input.startIndex
        let end = input.endIndex

        while i < end {
            let ch = input[i]

            // String literals — pass through unchanged (and ignore // or /* inside)
            if ch == "\"" {
                let (str, next) = try readString(input, from: i)
                output += str
                i = next
                continue
            }

            // Block comment /* */
            if ch == "/" && input.index(after: i) < end,
               input[input.index(after: i)] == "*" {
                let (_, next) = try skipBlockComment(input, from: i)
                i = next
                continue
            }

            // Line comment // ...
            if ch == "/" && input.index(after: i) < end,
               input[input.index(after: i)] == "/" {
                // Skip until end of line
                i = input.index(after: i) // skip second /
                while i < end && input[i] != "\n" {
                    i = input.index(after: i)
                }
                if i < end {
                    output.append("\n")
                    i = input.index(after: i)
                }
                continue
            }

            output.append(ch)
            i = input.index(after: i)
        }

        // Strip trailing commas before ] or }
        output = stripTrailingCommas(output)

        return output
    }

    // MARK: - String literal (preserves content, handles escapes)

    private static func readString(_ s: String, from start: String.Index) throws -> (String, String.Index) {
        var i = s.index(after: start) // skip opening "
        var result = "\""
        while i < s.endIndex {
            let ch = s[i]
            if ch == "\\" {
                // Escape sequence — include both chars
                result.append(ch)
                i = s.index(after: i)
                if i < s.endIndex {
                    result.append(s[i])
                }
            } else if ch == "\"" {
                result.append(ch)
                return (result, s.index(after: i))
            } else {
                result.append(ch)
            }
            i = s.index(after: i)
        }
        throw JSON5NormalizeError.unterminatedString
    }

    // MARK: - Block comment skip

    private static func skipBlockComment(_ s: String, from start: String.Index) throws -> (String, String.Index) {
        var i = s.index(start, offsetBy: 2) // skip /*
        while i < s.endIndex {
            if s[i] == "*" && s.index(after: i) < s.endIndex && s[s.index(after: i)] == "/" {
                return ("", s.index(i, offsetBy: 2))
            }
            i = s.index(after: i)
        }
        throw JSON5NormalizeError.unterminatedBlockComment
    }

    // MARK: - Trailing comma removal

    /// Strips trailing commas that appear immediately before `}` or `]`.
    /// Operates on the token stream so commas inside strings are untouched.
    private static func stripTrailingCommas(_ input: String) -> String {
        var output = ""
        var i = input.startIndex
        let end = input.endIndex

        while i < end {
            let ch = input[i]

            // Pass through strings unchanged
            if ch == "\"" {
                // Fast scan to closing quote, respecting backslash escapes
                output.append(ch)
                i = input.index(after: i)
                var escape = false
                while i < end {
                    let c = input[i]
                    output.append(c)
                    if escape {
                        escape = false
                    } else if c == "\\" {
                        escape = true
                    } else if c == "\"" {
                        i = input.index(after: i)
                        break
                    }
                    i = input.index(after: i)
                }
                continue
            }

            // Detect comma followed only by whitespace and then } or ]
            if ch == "," {
                let afterComma = input.index(after: i)
                if findClosingBrace(input, after: afterComma) != nil {
                    // Skip the comma and any whitespace
                    i = afterComma
                    while i < end && input[i].isWhitespace {
                        i = input.index(after: i)
                    }
                    // i now points to } or ]
                    output.append(input[i])
                    i = input.index(after: i)
                    continue
                }
            }

            output.append(ch)
            i = input.index(after: i)
        }

        return output
    }

    /// Starting from `after` (which should be just after a comma), scan forward
    /// through whitespace. If the next non-whitespace char is } or ], return its
    /// index. Otherwise return nil.
    private static func findClosingBrace(_ s: String, after: String.Index) -> String.Index? {
        var i = after
        while i < s.endIndex {
            if s[i].isWhitespace {
                i = s.index(after: i)
                continue
            }
            if s[i] == "}" || s[i] == "]" {
                return i
            }
            return nil
        }
        return nil
    }
}
