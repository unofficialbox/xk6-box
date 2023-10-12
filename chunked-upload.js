import box from 'k6/x/box';
import { Trend } from 'k6/metrics';

import { htmlReport } from "https://raw.githubusercontent.com/benc-uk/k6-reporter/main/dist/bundle.js";

const uploadEvent = new Trend('upload_event', true);


export function setup() {
    console.log('JS - Setup....')
    const baseFolderId = "230061918097";

    console.log('JS - Base folder id: ', baseFolderId)
    const config = {
        clientId: "141uw2duh1es7upm46n0e44sku5qerpn",
        clientSecret: "gZ4L21GRMHOptnU2R2R4zvlvJ9TK4hZz",
        boxSubjectType: "user",
        boxSubjectId: "385982796"
    }

    console.log('JS - Found config: ', config);
    const accessToken = box.accessToken(config);
    console.log('JS - Found Access token: ', accessToken)


    const folderId = box.createBaseFolder(accessToken, baseFolderId);
    console.log('Created new base folder with id: ', folderId);

    const filePath = `${__ENV.FILE_PATH}`;
    const fileInfo = box.getFileInfo(filePath)
    console.log('JS - Found file info: ', fileInfo);
    const fileName = fileInfo[0];
    const fileHash = fileInfo[1];
    const fileSize = fileInfo[2];

    return {
            accessToken: accessToken,
            folderId: folderId,
            fileName: fileName,
            fileHash: fileHash,
            fileSize: fileSize
        };
}

export default function (data) {
    console.log('JS - Found data: ', data);
    const filePath = `${__ENV.FILE_PATH}`;
    let { accessToken, fileSize, fileName, fileHash, folderId } = data; 
    console.log('JS - Found file path: ', filePath);
    // const uploadRes = box.chunkedUpload(data.accessToken, data.folderId, filePath);

    uploadEvent.add(1, { tag: 'create_session_before' });

    const createUploadSessionRes = box.createUploadSession(fileName, fileSize, folderId, accessToken);
    console.log('JS - Found upload res: ', createUploadSessionRes);
    uploadEvent.add(2, { tag: 'create_session_after' });

    uploadEvent.add(3, { tag: 'upload_parts_before' });

    const uploadPartsRes = box.uploadParts(createUploadSessionRes, filePath, fileSize, accessToken);
    console.log('JS - Found upload parts res: ', uploadPartsRes);
    uploadEvent.add(4, { tag: 'upload_parts_after' });

    const commitPayload = {
        parts: uploadPartsRes
    }

    uploadEvent.add(5, { tag: 'commit_session_before' });

    const commitRes = box.commitUploadSession(accessToken, fileHash, createUploadSessionRes.session_endpoints.commit, JSON.stringify(commitPayload));    
    console.log('JS - Found commit res: ', commitRes);
    uploadEvent.add(6, { tag: 'commit_session_after' });

}

export function handleSummary(data) {
    return {
      "summary.html": htmlReport(data),
    };
  }

