//
//  MenubarEvalView.swift
//  MeeptMenuBar
//

import SwiftUI

struct MenubarEvalView: View {
    @ObservedObject var evalVM: EvalBadgeManager

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Image(systemName: "terminal.fill")
                    .foregroundColor(.blue)
                    .font(.system(size: 10))
                Text("eval")
                    .font(.caption)
                    .fontWeight(.medium)
                Spacer()
                if evalVM.failCount > 0 {
                    Text("\(evalVM.failCount)")
                        .font(.caption2)
                        .foregroundColor(.red)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(Color.red.opacity(0.15))
                        .cornerRadius(3)
                }
            }
            if let lastFail = evalVM.lastFail {
                Text(lastFail)
                    .font(.caption2)
                    .foregroundColor(.secondary)
                    .lineLimit(1)
            } else if evalVM.failCount == 0 {
                Text("ok")
                    .font(.caption2)
                    .foregroundColor(.green)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
    }
}
