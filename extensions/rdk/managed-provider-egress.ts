import {
	type Api,
	type AssistantMessageEventStream,
	anthropicMessagesApi,
	type Context,
	type Model,
	openAICompletionsApi,
	openAIResponsesApi,
	type ProviderStreams,
	type SimpleStreamOptions,
} from "@earendil-works/pi-ai/compat";

import { modelEgressFetch } from "./model-egress.mjs";

function providerStreams(api: string): ProviderStreams | undefined {
	switch (api) {
		case "anthropic-messages": return anthropicMessagesApi();
		case "openai-completions": return openAICompletionsApi();
		case "openai-responses": return openAIResponsesApi();
		// Pi 0.84.1 intentionally rejects custom fetch for Google. Keep this
		// explicit so a future upstream change can be adopted with a test.
		case "google-generative-ai": return undefined;
		default: return undefined;
	}
}

export function createManagedProviderEgressStream(
	providerId: string,
	api: string,
	socketPath: string,
): ((model: Model<Api>, context: Context, options?: SimpleStreamOptions) => AssistantMessageEventStream) | undefined {
	const streams = providerStreams(api);
	if (!streams) return undefined;
	return (model, context, options) => streams.streamSimple(model, context, {
		...options,
		apiKey: "hobot-model-egress",
		fetch: (input, init) => modelEgressFetch(socketPath, providerId, input, init),
	});
}
