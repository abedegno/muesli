import SwiftUI

@main
struct MuesliApp: App {
    @StateObject private var environment = AppEnvironment.live()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(environment)
        }
    }
}

struct RootView: View {
    @EnvironmentObject private var environment: AppEnvironment

    var body: some View {
        Group {
            switch environment.sessionStore.state {
            case .signedIn(let configuration):
                if let model = environment.makeNotesListModel() {
                    NotesListView(configuration: configuration, model: model)
                } else {
                    SignInView()
                }
            default:
                SignInView()
            }
        }
    }
}
