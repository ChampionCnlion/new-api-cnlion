/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Form,
  Input,
  Modal,
  Space,
  Table,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { API, copy, showError, showSuccess, timestamp2string } from '../../helpers';
import { useTranslation } from 'react-i18next';

const DEFAULT_PAGE_SIZE = 10;

const statusMap = {
  1: { color: 'green', text: '未使用' },
  2: { color: 'grey', text: '已禁用' },
  3: { color: 'orange', text: '已使用' },
};

const InviteCodePage = () => {
  const { t } = useTranslation();
  const [inviteCodes, setInviteCodes] = useState([]);
  const [loading, setLoading] = useState(false);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const [createVisible, setCreateVisible] = useState(false);
  const [createCount, setCreateCount] = useState(10);
  const [generatedKeys, setGeneratedKeys] = useState([]);

  const loadInviteCodes = async (page = activePage, size = pageSize, keyword = searchKeyword) => {
    setLoading(true);
    try {
      const path =
        keyword.trim() === ''
          ? `/api/invite-code/?p=${page}&page_size=${size}`
          : `/api/invite-code/search?keyword=${encodeURIComponent(keyword)}&p=${page}&page_size=${size}`;
      const res = await API.get(path);
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setInviteCodes(data.items || []);
      setTotal(data.total || 0);
      setActivePage(data.page <= 0 ? 1 : data.page);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadInviteCodes(1, pageSize, '');
  }, []);

  const copyInviteCodes = async (keys) => {
    const text = keys.join('\n');
    if (await copy(text)) {
      showSuccess(t('已复制到剪贴板！'));
      return;
    }
    Modal.error({
      title: t('无法复制到剪贴板，请手动复制'),
      content: text,
      size: 'large',
    });
  };

  const buildInviteLink = (key) =>
    `${window.location.origin}/register?invite_code=${encodeURIComponent(key)}`;

  const handleCreate = async () => {
    setLoading(true);
    try {
      const res = await API.post('/api/invite-code/', { count: createCount });
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setCreateVisible(false);
      setGeneratedKeys(data || []);
      showSuccess(t('邀请码生成成功'));
      await loadInviteCodes(1, pageSize, searchKeyword);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  const handleSearch = async () => {
    await loadInviteCodes(1, pageSize, searchKeyword);
  };

  const handleStatusChange = async (record, status) => {
    setLoading(true);
    try {
      const res = await API.put('/api/invite-code/', {
        id: record.id,
        status,
      });
      const { success, message } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('操作成功完成！'));
      await loadInviteCodes(activePage, pageSize, searchKeyword);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = (record) => {
    Modal.confirm({
      title: t('确定删除这个邀请码？'),
      content: t('删除后无法恢复。'),
      onOk: async () => {
        setLoading(true);
        try {
          const res = await API.delete(`/api/invite-code/${record.id}`);
          const { success, message } = res.data;
          if (!success) {
            showError(message);
            return;
          }
          showSuccess(t('删除成功'));
          const nextPage =
            inviteCodes.length === 1 && activePage > 1 ? activePage - 1 : activePage;
          await loadInviteCodes(nextPage, pageSize, searchKeyword);
        } catch (error) {
          showError(error.message);
        } finally {
          setLoading(false);
        }
      },
    });
  };

  const columns = useMemo(
    () => [
      {
        title: t('ID'),
        dataIndex: 'id',
      },
      {
        title: t('邀请码'),
        dataIndex: 'key',
        render: (text) => <Typography.Text>{text}</Typography.Text>,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        render: (status) => {
          const statusInfo = statusMap[status] || {
            color: 'grey',
            text: '未知',
          };
          return <Tag color={statusInfo.color}>{t(statusInfo.text)}</Tag>;
        },
      },
      {
        title: t('创建时间'),
        dataIndex: 'created_time',
        render: (text) => (text ? timestamp2string(text) : '-'),
      },
      {
        title: t('使用用户'),
        dataIndex: 'used_user_id',
        render: (text) => (text || '-'),
      },
      {
        title: t('使用时间'),
        dataIndex: 'used_time',
        render: (text) => (text ? timestamp2string(text) : '-'),
      },
      {
        title: t('操作'),
        dataIndex: 'operate',
        render: (_, record) => (
          <Space>
            <Button size='small' onClick={() => copyInviteCodes([record.key])}>
              {t('复制')}
            </Button>
            <Button
              size='small'
              onClick={() => copyInviteCodes([buildInviteLink(record.key)])}
            >
              {t('复制链接')}
            </Button>
            {record.status === 1 && (
              <Button
                size='small'
                type='tertiary'
                theme='warning'
                onClick={() => handleStatusChange(record, 2)}
              >
                {t('禁用')}
              </Button>
            )}
            {record.status === 2 && (
              <Button size='small' onClick={() => handleStatusChange(record, 1)}>
                {t('启用')}
              </Button>
            )}
            <Button
              size='small'
              type='tertiary'
              theme='danger'
              onClick={() => handleDelete(record)}
            >
              {t('删除')}
            </Button>
          </Space>
        ),
      },
    ],
    [t, activePage, pageSize, searchKeyword, inviteCodes],
  );

  return (
    <div className='mt-[60px] px-2'>
      <Card>
        <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
          <Space>
            <Input
              value={searchKeyword}
              onChange={(value) => setSearchKeyword(value)}
              placeholder={t('搜索邀请码或 ID')}
              showClear
              onEnterPress={handleSearch}
            />
            <Button onClick={handleSearch}>{t('搜索')}</Button>
          </Space>
          <Button type='primary' onClick={() => setCreateVisible(true)}>
            {t('生成邀请码')}
          </Button>
        </div>

        <Table
          rowKey='id'
          loading={loading}
          columns={columns}
          dataSource={inviteCodes}
          pagination={{
            currentPage: activePage,
            pageSize,
            total,
            onPageChange: (page) => {
              setActivePage(page);
              loadInviteCodes(page, pageSize, searchKeyword);
            },
            onPageSizeChange: (size) => {
              setPageSize(size);
              setActivePage(1);
              loadInviteCodes(1, size, searchKeyword);
            },
          }}
        />
      </Card>

      <Modal
        title={t('生成邀请码')}
        visible={createVisible}
        onCancel={() => setCreateVisible(false)}
        onOk={handleCreate}
      >
        <Form>
          <Form.InputNumber
            field='count'
            label={t('生成数量')}
            min={1}
            max={100}
            step={1}
            value={createCount}
            onChange={(value) => setCreateCount(value || 1)}
          />
        </Form>
      </Modal>

      <Modal
        title={t('已生成的邀请码')}
        visible={generatedKeys.length > 0}
        onCancel={() => setGeneratedKeys([])}
        footer={
          <Space>
            <Button onClick={() => copyInviteCodes(generatedKeys)}>
              {t('复制全部')}
            </Button>
            <Button type='primary' onClick={() => setGeneratedKeys([])}>
              {t('关闭')}
            </Button>
          </Space>
        }
      >
        <TextArea value={generatedKeys.join('\n')} autosize readOnly />
      </Modal>
    </div>
  );
};

export default InviteCodePage;
