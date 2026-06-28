// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Modal } from "@/app/modals/modal";
import { useT } from "@/app/i18n/react";
import { modalsModel } from "@/app/store/modalmodel";

import { ReactNode } from "react";
import "./messagemodal.scss";

const MessageModal = ({ children, okLabel }: { children: ReactNode; okLabel?: string }) => {
    const tt = useT();

    function closeModal() {
        modalsModel.popModal();
    }

    return (
        <Modal
            className="message-modal"
            onOk={() => closeModal()}
            onClose={() => closeModal()}
            okLabel={okLabel ?? tt("Ok")}
        >
            {children}
        </Modal>
    );
};

MessageModal.displayName = "MessageModal";

export { MessageModal };
