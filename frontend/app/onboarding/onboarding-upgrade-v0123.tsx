// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_12_3_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave AI model upgrade to GPT-5.1, new secret management features, and improved terminal input handling for interactive CLI tools."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sparkles"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Wave AI Updates")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("GPT-5.1 Model")}</strong> -{" "}
                                {tt("Upgraded to OpenAI's GPT-5.1 model for improved responses")}
                            </li>
                            <li>
                                <strong>{tt("Thinking Mode Toggle")}</strong> -{" "}
                                {tt("New dropdown to select between Quick, Balanced, and Deep thinking modes")}
                            </li>
                            <li>{tt("Fixed path mismatch issue when restoring AI write file backups")}</li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-terminal"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Terminal Improvements")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Enhanced Input Handling")}</strong> -{" "}
                                {tt("Better support for CLI tools like Claude Code")}
                            </li>
                            <li>
                                <strong>{tt("Image Paste Support")}</strong> -{" "}
                                {tt("Paste images directly into terminal (saved to temp files)")}
                            </li>
                            <li>{tt("Shift+Enter now inserts newlines by default for multi-line commands")}</li>
                            <li>{tt("Fixed duplicate text issue when switching input methods (IME)")}</li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-key"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Secret Store")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Secret Management Widget")}</strong> -{" "}
                                {tt("Store and manage sensitive credentials securely")}
                            </li>
                            <li>
                                {tt("Access secrets via CLI with")}{" "}
                                <span className="font-mono">wsh secret list/get/set</span> {tt("commands")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_12_3_Content.displayName = "UpgradeOnboardingModal_v0_12_3_Content";

export { UpgradeOnboardingModal_v0_12_3_Content };