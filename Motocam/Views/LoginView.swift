import SwiftUI

struct LoginView: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session

    var hint: String?
    @State private var email = ""
    @State private var code = ""
    @State private var agreed = false
    @State private var termsVersion = "1.0"
    @State private var termsMarkdown = ""
    @State private var showTerms = false
    @State private var message: String?
    @State private var busy = false
    @State private var cooldown = 0
    @State private var ticker: Task<Void, Never>?

    var body: some View {
        NavigationStack {
            Form {
                if let hint, !hint.isEmpty {
                    Section {
                        Text(hint)
                    }
                }
                Section {
                    TextField("邮箱", text: $email)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.emailAddress)
                        .autocorrectionDisabled()
                    Button(cooldown > 0 ? "\(cooldown) 秒后可重发" : "获取激活码") {
                        Task { await requestCode() }
                    }
                    .disabled(busy || cooldown > 0 || !email.contains("@"))
                    TextField("6 位激活码", text: $code)
                        .keyboardType(.numberPad)
                        .onChange(of: code) { _, value in
                            let digits = value.filter(\.isNumber)
                            if digits != value || digits.count > 6 {
                                code = String(digits.prefix(6))
                            }
                        }
                }
                Section {
                    Toggle("我已阅读并同意《用户协议与免责声明》", isOn: $agreed)
                    Button("查看《用户协议与免责声明》") {
                        Task {
                            let doc = await TermsLoader.load(baseURL: settings.communityBaseURL)
                            termsVersion = doc.version
                            termsMarkdown = doc.markdown
                            showTerms = true
                        }
                    }
                }
                Section {
                    Button("注册 / 登录") {
                        Task { await verify() }
                    }
                    .disabled(busy || !agreed || code.count != 6)
                }
                if let message {
                    Section {
                        Text(message)
                    }
                }
            }
            .navigationTitle("注册 / 登录")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { session.showLogin = false }
                }
            }
            .sheet(isPresented: $showTerms) {
                NavigationStack {
                    ScrollView {
                        Text(termsMarkdown)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding()
                    }
                    .navigationTitle("用户协议与免责声明")
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("关闭") { showTerms = false }
                        }
                    }
                }
            }
        }
    }

    private func requestCode() async {
        busy = true
        defer { busy = false }
        let doc = await TermsLoader.load(baseURL: settings.communityBaseURL)
        termsVersion = doc.version
        do {
            try await AuthClient.requestCode(baseURL: settings.communityBaseURL, email: email.trimmingCharacters(in: .whitespacesAndNewlines))
            message = "激活码已发送，请查看邮箱。"
            startCooldown()
        } catch {
            message = error.localizedDescription
        }
    }

    private func verify() async {
        busy = true
        defer { busy = false }
        let doc = await TermsLoader.load(baseURL: settings.communityBaseURL)
        termsVersion = doc.version
        do {
            let logged = try await AuthClient.verify(
                baseURL: settings.communityBaseURL,
                email: email.trimmingCharacters(in: .whitespacesAndNewlines),
                code: code,
                termsVersion: doc.version
            )
            session.adopt(logged)
            _ = await UploadQueue.flush(settings: settings)
        } catch {
            message = error.localizedDescription
        }
    }

    private func startCooldown() {
        ticker?.cancel()
        cooldown = 60
        ticker = Task {
            while !Task.isCancelled && cooldown > 0 {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                if Task.isCancelled { return }
                cooldown -= 1
            }
        }
    }
}

struct TermsReacceptView: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @State private var markdown = "正在加载协议…"
    @State private var version = "1.0"
    @State private var agreed = false
    @State private var message: String?
    @State private var busy = false

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 12) {
                Text("协议已更新到 \(version)。同意后才能继续使用账号相关功能。")
                    .font(.subheadline)
                ScrollView {
                    Text(markdown)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                Toggle("我已阅读并同意《用户协议与免责声明》", isOn: $agreed)
                Button("同意并继续") {
                    Task { await accept() }
                }
                .buttonStyle(.borderedProminent)
                .disabled(!agreed || busy)
                if let message {
                    Text(message).foregroundStyle(.red)
                }
            }
            .padding()
            .navigationTitle("用户协议")
            .navigationBarTitleDisplayMode(.inline)
        }
        .interactiveDismissDisabled(true)
        .task {
            let doc = await TermsLoader.load(baseURL: settings.communityBaseURL)
            markdown = doc.markdown
            version = doc.version
        }
    }

    private func accept() async {
        busy = true
        defer { busy = false }
        do {
            try await AuthClient.accept(baseURL: settings.communityBaseURL, version: version)
            session.noteAccepted(version)
        } catch {
            message = error.localizedDescription
        }
    }
}

struct TermsReaderView: View {
    var markdown: String
    var onClose: () -> Void

    var body: some View {
        NavigationStack {
            ScrollView {
                Text(markdown)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding()
            }
            .navigationTitle("用户协议与免责声明")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("关闭", action: onClose)
                }
            }
        }
    }
}
