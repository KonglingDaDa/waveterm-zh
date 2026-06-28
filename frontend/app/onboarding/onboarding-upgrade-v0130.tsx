// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_13_0_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave v0.13 brings local AI support, bring-your-own-key (BYOK), a redesigned configuration system, and improved terminal functionality."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sparkles"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Local AI & BYOK")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("OpenAI-Compatible API")}</strong> -{" "}
                                {tt(
                                    "Connect to Ollama, LM Studio, vLLM, OpenRouter, and other local or hosted models"
                                )}
                            </li>
                            <li>
                                <strong>{tt("Google Gemini")}</strong> - {tt("Native support for Gemini models")}
                            </li>
                            <li>
                                <strong>{tt("Provider Presets")}</strong> -{" "}
                                {tt("Built-in configs for OpenAI, OpenRouter, Google, Azure, and custom endpoints")}
                            </li>
                            <li>
                                <strong>{tt("Multiple AI Modes")}</strong> -{" "}
                                {tt("Easily switch between models and providers")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sliders"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Configuration Widget")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("New Config Interface")}</strong> -{" "}
                                {tt("Dedicated widget accessible from the sidebar")}
                            </li>
                            <li>
                                <strong>{tt("Better Organization")}</strong> -{" "}
                                {tt("Browse and edit settings with improved validation and error handling")}
                            </li>
                            <li>
                                <strong>{tt("Integrated Secrets")}</strong> -{" "}
                                {tt("Manage API keys and credentials from the config widget")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-terminal"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Terminal Updates")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Bracketed Paste Mode")}</strong> -{" "}
                                {tt("Enabled by default for better multi-line paste behavior")}
                            </li>
                            <li>
                                <strong>{tt("Windows Paste Fix")}</strong> -{" "}
                                {tt("Ctrl+V now works as standard paste on Windows")}
                            </li>
                            <li>
                                <strong>{tt("SSH Password Storage")}</strong> -{" "}
                                {tt("Store SSH passwords in Wave's secret store")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_13_0_Content.displayName = "UpgradeOnboardingModal_v0_13_0_Content";

export { UpgradeOnboardingModal_v0_13_0_Content };